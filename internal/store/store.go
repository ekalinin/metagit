// Package store keeps metagit settings and state in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	// ErrNotFound is returned when a record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when a record violates a uniqueness rule.
	ErrConflict = errors.New("already exists")
)

//go:embed migrations/*.sql
var migrations embed.FS

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store gives typed access to the database.
type Store struct {
	db *sql.DB
	q  querier
}

// Open opens (creating if needed) the database file and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	// The database keeps encrypted credentials and password hashes. SQLite
	// gives -wal and -shm files the mode of the database file.
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes writers and avoids SQLITE_BUSY. Callers must
	// close result sets before running the next query.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, q: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// WithTx runs fn in a transaction. fn gets a Store bound to the transaction.
func (s *Store) WithTx(ctx context.Context, fn func(tx *Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&Store{db: s.db, q: tx}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	// A database migrated by a newer metagit is left alone: this binary does
	// not know the newer schema.
	newest, known := 0, 0
	for v := range applied {
		newest = max(newest, v)
	}
	for _, name := range names {
		v, err := migrationVersion(name)
		if err != nil {
			return err
		}
		known = max(known, v)
	}
	if newest > known {
		return fmt.Errorf("database schema version %d is newer than this metagit supports (%d): "+
			"install a newer metagit or restore the data directory from a backup", newest, known)
	}
	for _, name := range names {
		base := filepath.Base(name)
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = s.WithTx(ctx, func(tx *Store) error {
			if _, err := tx.q.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("migration %s: %w", base, err)
			}
			_, err := tx.q.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, ms(time.Now()))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// migrationVersion returns NNNN of migrations/NNNN_name.sql.
func migrationVersion(name string) (int, error) {
	base := filepath.Base(name)
	prefix, _, _ := strings.Cut(base, "_")
	v, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("bad migration name %s", base)
	}
	return v, nil
}

// ms converts time to unix milliseconds; the zero time becomes NULL.
func ms(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func fromMS(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64)
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func isUnique(err error) bool {
	if e, ok := errors.AsType[*sqlite.Error](err); ok {
		code := e.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// collect runs a query and scans every row with scan.
func collect[T any](ctx context.Context, q querier, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

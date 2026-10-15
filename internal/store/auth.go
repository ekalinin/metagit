package store

import (
	"context"
	"database/sql"
	"time"
)

type scanner interface {
	Scan(dest ...any) error
}

// Owner is the single user of metagit.
type Owner struct {
	Name         string
	PasswordHash string
	CreatedAt    time.Time
}

// GetOwner returns the owner or ErrNotFound.
func (s *Store) GetOwner(ctx context.Context) (*Owner, error) {
	var o Owner
	var created sql.NullInt64
	err := s.q.QueryRowContext(ctx, `SELECT name, password_hash, created_at FROM owner WHERE id = 1`).
		Scan(&o.Name, &o.PasswordHash, &created)
	if err != nil {
		return nil, notFound(err)
	}
	o.CreatedAt = fromMS(created)
	return &o, nil
}

// CreateOwner creates the owner; ErrConflict if it already exists.
func (s *Store) CreateOwner(ctx context.Context, name, passwordHash string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO owner (id, name, password_hash, created_at) VALUES (1, ?, ?, ?)`,
		name, passwordHash, ms(time.Now()))
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// UpdateOwnerPassword replaces the owner's password hash.
func (s *Store) UpdateOwnerPassword(ctx context.Context, passwordHash string) error {
	return affected(s.q.ExecContext(ctx, `UPDATE owner SET password_hash = ? WHERE id = 1`, passwordHash))
}

// CreateSession stores a session by the hash of its identifier.
func (s *Store) CreateSession(ctx context.Context, idHash string, expires time.Time) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO sessions (id_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		idHash, ms(time.Now()), ms(expires))
	return err
}

// SessionValid reports whether the session exists and has not expired.
func (s *Store) SessionValid(ctx context.Context, idHash string, now time.Time) (bool, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id_hash = ? AND expires_at > ?`,
		idHash, now.UnixMilli()).Scan(&n)
	return n > 0, err
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash)
	return err
}

// DeleteExpiredSessions removes sessions that expired before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UnixMilli())
	return err
}

// AccessToken authorizes git over HTTP. Only the hash of the value is stored.
type AccessToken struct {
	ID         int64
	Name       string
	Hash       string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

func scanToken(r scanner) (AccessToken, error) {
	var t AccessToken
	var created, used sql.NullInt64
	err := r.Scan(&t.ID, &t.Name, &t.Hash, &created, &used)
	t.CreatedAt, t.LastUsedAt = fromMS(created), fromMS(used)
	return t, err
}

const tokenColumns = `id, name, token_hash, created_at, last_used_at`

// CreateToken stores a new access token.
func (s *Store) CreateToken(ctx context.Context, name, hash string) (AccessToken, error) {
	now := time.Now()
	res, err := s.q.ExecContext(ctx, `INSERT INTO access_tokens (name, token_hash, created_at) VALUES (?, ?, ?)`,
		name, hash, ms(now))
	if err != nil {
		if isUnique(err) {
			return AccessToken{}, ErrConflict
		}
		return AccessToken{}, err
	}
	id, err := res.LastInsertId()
	return AccessToken{ID: id, Name: name, Hash: hash, CreatedAt: time.UnixMilli(now.UnixMilli())}, err
}

// ListTokens returns all access tokens.
func (s *Store) ListTokens(ctx context.Context) ([]AccessToken, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (AccessToken, error) { return scanToken(r) },
		`SELECT `+tokenColumns+` FROM access_tokens ORDER BY id`)
}

// TokenByHash finds a token by the hash of its value.
func (s *Store) TokenByHash(ctx context.Context, hash string) (*AccessToken, error) {
	t, err := scanToken(s.q.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM access_tokens WHERE token_hash = ?`, hash))
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

// TouchToken records the last use of a token.
func (s *Store) TouchToken(ctx context.Context, id int64, now time.Time) error {
	_, err := s.q.ExecContext(ctx, `UPDATE access_tokens SET last_used_at = ? WHERE id = ?`, ms(now), id)
	return err
}

// DeleteToken revokes a token.
func (s *Store) DeleteToken(ctx context.Context, id int64) error {
	return affected(s.q.ExecContext(ctx, `DELETE FROM access_tokens WHERE id = ?`, id))
}

// SSHKey is a public key of the owner for git over SSH.
type SSHKey struct {
	ID          int64
	Name        string
	PublicKey   string
	Fingerprint string
	CreatedAt   time.Time
}

func scanSSHKey(r scanner) (SSHKey, error) {
	var k SSHKey
	var created sql.NullInt64
	err := r.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &created)
	k.CreatedAt = fromMS(created)
	return k, err
}

const sshKeyColumns = `id, name, public_key, fingerprint, created_at`

// CreateSSHKey stores a key; ErrConflict if the same key is already stored.
func (s *Store) CreateSSHKey(ctx context.Context, name, publicKey, fingerprint string) (SSHKey, error) {
	now := time.Now()
	res, err := s.q.ExecContext(ctx, `INSERT INTO ssh_keys (name, public_key, fingerprint, created_at) VALUES (?, ?, ?, ?)`,
		name, publicKey, fingerprint, ms(now))
	if err != nil {
		if isUnique(err) {
			return SSHKey{}, ErrConflict
		}
		return SSHKey{}, err
	}
	id, err := res.LastInsertId()
	return SSHKey{ID: id, Name: name, PublicKey: publicKey, Fingerprint: fingerprint, CreatedAt: time.UnixMilli(now.UnixMilli())}, err
}

// ListSSHKeys returns all keys.
func (s *Store) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (SSHKey, error) { return scanSSHKey(r) },
		`SELECT `+sshKeyColumns+` FROM ssh_keys ORDER BY id`)
}

// SSHKeyByFingerprint finds a key by its SHA256 fingerprint.
func (s *Store) SSHKeyByFingerprint(ctx context.Context, fingerprint string) (*SSHKey, error) {
	k, err := scanSSHKey(s.q.QueryRowContext(ctx, `SELECT `+sshKeyColumns+` FROM ssh_keys WHERE fingerprint = ?`, fingerprint))
	if err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

// DeleteSSHKey removes a key.
func (s *Store) DeleteSSHKey(ctx context.Context, id int64) error {
	return affected(s.q.ExecContext(ctx, `DELETE FROM ssh_keys WHERE id = ?`, id))
}

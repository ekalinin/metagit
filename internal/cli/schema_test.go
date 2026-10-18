package cli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/store"
)

// newerDatabase returns a data directory whose database was migrated by a
// newer metagit.
func newerDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), config.DBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(config.DBPath(dir)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO schema_migrations (version, applied_at) SELECT MAX(version) + 1, 0 FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	return dir, db
}

const newerSchema = "is newer than this metagit supports"

func TestOwnerCreateOnNewerDatabase(t *testing.T) {
	dir, db := newerDatabase(t)

	code, _, errOut := runCLI(t, "password-1\n", "owner", "create", "--data-dir", dir, "--name", "eugene")
	if code != 1 || !strings.Contains(errOut, newerSchema) {
		t.Fatalf("owner create: code %d, err %q", code, errOut)
	}
	var owners int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM owner`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 0 {
		t.Fatalf("%d owners created", owners)
	}
}

func TestServeOnNewerDatabase(t *testing.T) {
	dir, _ := newerDatabase(t)

	// serve must stop at once; if it starts, it runs until the timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"serve", "--data-dir", dir, "--http-addr", "127.0.0.1:0", "--ssh-addr", "127.0.0.1:0"}, Env{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(string) string { return "" },
	})
	if ctx.Err() != nil {
		t.Fatalf("serve started on a newer database:\n%s", stderr.String())
	}
	if code != 1 || !strings.Contains(stderr.String(), "cannot start") || !strings.Contains(stderr.String(), newerSchema) {
		t.Fatalf("serve: code %d, err %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "metagit started") {
		t.Fatalf("serve listened: %q", stderr.String())
	}
}

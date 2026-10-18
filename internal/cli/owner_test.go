package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/auth"
	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/store"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), args, Env{
		Stdin:  strings.NewReader(stdin),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(string) string { return "" },
	})
	return code, stdout.String(), stderr.String()
}

func TestOwnerCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	code, out, errOut := runCLI(t, "password-1\n", "owner", "create", "--data-dir", dir, "--name", "eugene")
	if code != 0 || !strings.Contains(out, "created") {
		t.Fatalf("create: code %d, out %q, err %q", code, out, errOut)
	}
	code, out, errOut = runCLI(t, "password-2\n", "owner", "create", "--data-dir", dir, "--name", "eugene")
	if code != 0 || !strings.Contains(out, "password of owner") {
		t.Fatalf("change password: code %d, out %q, err %q", code, out, errOut)
	}
	code, _, errOut = runCLI(t, "password-3\n", "owner", "create", "--data-dir", dir, "--name", "someone")
	if code == 0 || !strings.Contains(errOut, "owner already exists") {
		t.Fatalf("second owner: code %d, err %q", code, errOut)
	}

	st, err := store.Open(t.Context(), config.DBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := auth.New(st).Login(t.Context(), "eugene", "password-2", "127.0.0.1:1"); err != nil {
		t.Fatalf("password was not changed: %v", err)
	}
}

func TestOwnerCreateUsage(t *testing.T) {
	if code, _, _ := runCLI(t, "", "owner", "create"); code != 2 {
		t.Fatalf("missing --name: code %d", code)
	}
	if code, _, errOut := runCLI(t, "", "owner", "create", "--data-dir", t.TempDir(), "--name", "x"); code != 1 || !strings.Contains(errOut, "empty password") {
		t.Fatalf("empty password: code %d, err %q", code, errOut)
	}
	if code, _, _ := runCLI(t, "", "owner"); code != 2 {
		t.Fatalf("owner without create: code %d", code)
	}
}

func TestHelp(t *testing.T) {
	code, out, _ := runCLI(t, "", "--help")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	for _, cmd := range []string{"serve", "owner create", "hook", "askpass", "version"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("help does not mention %q", cmd)
		}
	}
	if code, _, _ := runCLI(t, "", "nope"); code != 2 {
		t.Fatalf("unknown command: code %d", code)
	}
}

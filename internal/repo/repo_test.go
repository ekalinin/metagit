package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/gitcmd"
)

func newManager(t *testing.T) *Manager {
	dir := t.TempDir()
	return &Manager{
		ReposDir: filepath.Join(dir, "repos"),
		HooksDir: filepath.Join(dir, "hooks"),
		Git:      &gitcmd.Runner{},
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"foo", "my-project", "a.b_c", "x1", "_tmp"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q must be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Foo", "a/b", "..", ".hidden", "-x", "a b", "проект", strings.Repeat("a", 101)} {
		if err := ValidateName(bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("%q must be invalid", bad)
		}
	}
}

func TestCreateEmptyRepository(t *testing.T) {
	m := newManager(t)
	ctx := t.Context()
	if err := m.Create(ctx, "foo"); err != nil {
		t.Fatal(err)
	}
	if !m.Exists("foo") {
		t.Fatal("repository is missing")
	}
	get := func(key string) string {
		v, err := m.Git.Output(ctx, gitcmd.Opts{Dir: m.Path("foo")}, "config", "--get", key)
		if err != nil {
			t.Fatalf("config %s: %v", key, err)
		}
		return v
	}
	if get("core.bare") != "true" || get("core.hooksPath") != m.HooksDir || get("core.logAllRefUpdates") != "always" {
		t.Fatal("repository is not configured")
	}

	clone := filepath.Join(t.TempDir(), "clone")
	out, err := exec.Command("git", "clone", "-q", m.Path("foo"), clone).CombinedOutput()
	if err != nil {
		t.Fatalf("clone of empty repository: %v: %s", err, out)
	}

	if err := m.Create(ctx, "foo"); !errors.Is(err, ErrExists) {
		t.Fatalf("second create: %v", err)
	}
	if err := m.Create(ctx, "Bad/Name"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("bad name: %v", err)
	}
	if err := m.Delete("foo"); err != nil {
		t.Fatal(err)
	}
	if m.Exists("foo") {
		t.Fatal("repository not deleted")
	}
}

func TestWriteHooks(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "it's metagit")
	if err := WriteHooks(filepath.Join(dir, "hooks"), binary); err != nil {
		t.Fatal(err)
	}
	for _, name := range HookNames {
		path := filepath.Join(dir, "hooks", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s is not executable: %v", name, info.Mode())
		}
		data, _ := os.ReadFile(path)
		want := "exec " + ShellQuote(binary) + " 'hook' '" + name + "' \"$@\"\n"
		if !strings.HasPrefix(string(data), "#!/bin/sh\n") || !strings.HasSuffix(string(data), want) {
			t.Fatalf("%s content:\n%s", name, data)
		}
	}

	// The script really runs the binary with the quoted path.
	fake := filepath.Join(dir, "it's metagit")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(filepath.Join(dir, "hooks", "pre-receive"), "x").Output()
	if err != nil || strings.TrimSpace(string(out)) != "hook pre-receive x" {
		t.Fatalf("script output %q, %v", out, err)
	}
}

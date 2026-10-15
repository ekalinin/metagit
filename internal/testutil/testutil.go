// Package testutil has helpers for tests that run the real git.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// GitEnv is the environment of a "user" git in tests: no user or system
// config, no prompts, fixed identity.
func GitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "SSH_AUTH_SOCK=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"LC_ALL=C",
	)
}

// GitErr runs git in dir and returns combined output.
func GitErr(dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(GitEnv(), extraEnv...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Git runs git in dir and fails the test on error. It returns trimmed output.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := GitErr(dir, nil, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// InitWork creates a non-bare repository with branch main.
func InitWork(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "work")
	Git(t, t.TempDir(), "init", "-q", "-b", "main", dir)
	return dir
}

// Commit makes an empty commit and returns its id.
func Commit(t testing.TB, dir, msg string) string {
	t.Helper()
	Git(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	return Git(t, dir, "rev-parse", "HEAD")
}

// RefOf returns the object of ref in the repository at dir, or "".
func RefOf(t testing.TB, dir, ref string) string {
	t.Helper()
	out, err := GitErr(dir, nil, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return out
}

// ShortTempDir returns a temporary directory with a short path, suitable for
// unix sockets.
func ShortTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
	buildOut  []byte
)

// Binary builds the metagit binary once per test process and returns its path.
func Binary(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "metagit-bin")
		if err != nil {
			buildErr = err
			return
		}
		buildPath = filepath.Join(dir, "metagit")
		cmd := exec.Command("go", "build", "-o", buildPath, "github.com/ekalinin/metagit/cmd/metagit")
		buildOut, buildErr = cmd.CombinedOutput()
	})
	if buildErr != nil {
		t.Fatalf("build metagit: %v\n%s", buildErr, buildOut)
	}
	return buildPath
}

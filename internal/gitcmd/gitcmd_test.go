package gitcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseForEachRef(t *testing.T) {
	out := []byte("1111111111111111111111111111111111111111 refs/heads/main\n" +
		"2222222222222222222222222222222222222222 refs/tags/v1.0\n")
	refs, err := ParseForEachRef(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []Ref{
		{Name: "refs/heads/main", Object: "1111111111111111111111111111111111111111"},
		{Name: "refs/tags/v1.0", Object: "2222222222222222222222222222222222222222"},
	}
	if len(refs) != len(want) || refs[0] != want[0] || refs[1] != want[1] {
		t.Fatalf("got %+v", refs)
	}
	if refs, err := ParseForEachRef(nil); err != nil || len(refs) != 0 {
		t.Fatalf("empty output: %v %v", refs, err)
	}
	if _, err := ParseForEachRef([]byte("garbage")); err == nil {
		t.Fatal("expected error for malformed line")
	}
}

func TestZeroOID(t *testing.T) {
	if !ZeroOID(strings.Repeat("0", 40)) || !ZeroOID(strings.Repeat("0", 64)) {
		t.Error("zero ids not detected")
	}
	if ZeroOID("") || ZeroOID("0000000000000000000000000000000000000001") {
		t.Error("non-zero id detected as zero")
	}
}

func TestTimeout(t *testing.T) {
	// "git hash-object --stdin" waits for stdin that never ends.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	defer r.Close()
	runner := &Runner{Timeout: 200 * time.Millisecond}
	start := time.Now()
	_, err = runner.Run(t.Context(), Opts{Dir: t.TempDir(), Stdin: r}, "hash-object", "--stdin")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took too long: %v", time.Since(start))
	}
}

func TestRunAndForEachRef(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	r := &Runner{}
	if _, err := r.Run(ctx, Opts{Dir: dir}, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	commit := func(msg string) string {
		t.Helper()
		env := []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}
		if _, err := r.Run(ctx, Opts{Dir: dir, Env: env}, "commit", "-q", "--allow-empty", "-m", msg); err != nil {
			t.Fatal(err)
		}
		oid, err := r.Output(ctx, Opts{Dir: dir}, "rev-parse", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		return oid
	}
	first := commit("first")
	second := commit("second")
	if _, err := r.Run(ctx, Opts{Dir: dir}, "tag", "v1", first); err != nil {
		t.Fatal(err)
	}

	refs, err := r.ForEachRef(ctx, filepath.Join(dir, ".git"), "refs/heads/", "refs/tags/")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0] != (Ref{"refs/heads/main", second}) || refs[1] != (Ref{"refs/tags/v1", first}) {
		t.Fatalf("refs = %+v", refs)
	}

	if ok, err := r.IsAncestor(ctx, Opts{Dir: dir}, first, second); err != nil || !ok {
		t.Fatalf("first must be ancestor of second: %v %v", ok, err)
	}
	if ok, err := r.IsAncestor(ctx, Opts{Dir: dir}, second, first); err != nil || ok {
		t.Fatalf("second must not be ancestor of first: %v %v", ok, err)
	}
	if !r.HasObject(ctx, dir, first) || r.HasObject(ctx, dir, strings.Repeat("a", 40)) {
		t.Fatal("HasObject is wrong")
	}
	if oid, err := r.ResolveRef(ctx, dir, "refs/heads/main"); err != nil || oid != second {
		t.Fatalf("resolve main: %q %v", oid, err)
	}
	if oid, err := r.ResolveRef(ctx, dir, "refs/heads/missing"); err != nil || oid != "" {
		t.Fatalf("resolve missing: %q %v", oid, err)
	}

	_, err = r.Run(ctx, Opts{Dir: dir}, "rev-parse", "--verify", "no-such-ref")
	gerr, ok := errors.AsType[*Error](err)
	if !ok || gerr.ExitCode == 0 || gerr.Stderr == "" {
		t.Fatalf("expected *Error with stderr, got %#v", err)
	}
}

func TestCheckTools(t *testing.T) {
	version, err := CheckTools(t.Context(), "git", "ssh")
	if err != nil {
		t.Fatalf("tools must be available in the test environment: %v", err)
	}
	if !strings.HasPrefix(version, "git version ") {
		t.Fatalf("version = %q", version)
	}
}

func TestCheckToolsWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := CheckTools(t.Context(), "git", "ssh")
	if err == nil || !strings.Contains(err.Error(), "git is not found") {
		t.Fatalf("expected clear error, got %v", err)
	}
}

func TestCheckToolsOldGit(t *testing.T) {
	git := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\necho git version 2.31.8\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CheckTools(t.Context(), git, "ssh")
	if err == nil || !strings.Contains(err.Error(), "git version 2.31.8 is too old: metagit needs git 2.32 or newer") {
		t.Fatalf("expected too old error, got %v", err)
	}
}

func TestParseGitVersion(t *testing.T) {
	cases := map[string][2]int{
		"git version 2.55.0":                 {2, 55},
		"git version 2.39.5 (Apple Git-154)": {2, 39},
		"git version 2.30.0.windows.1":       {2, 30},
	}
	for in, want := range cases {
		major, minor, err := ParseGitVersion(in)
		if err != nil || major != want[0] || minor != want[1] {
			t.Errorf("%q: %d.%d %v", in, major, minor, err)
		}
	}
	if _, _, err := ParseGitVersion("hello"); err == nil {
		t.Error("expected error")
	}
}

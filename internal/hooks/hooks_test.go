package hooks

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ekalinin/metagit/internal/gitcmd"
)

type fakeHandler struct {
	mu    sync.Mutex
	calls []Request
	allow bool
}

func (f *fakeHandler) record(hook, project, pushID string, updates []Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Request{Hook: hook, Project: project, PushID: pushID, Updates: updates})
}

func (f *fakeHandler) PreReceive(_ context.Context, project, pushID string, updates []Update) (Response, error) {
	f.record(PreReceive, project, pushID, updates)
	return Response{Allow: f.allow, Messages: []string{"pre: " + project}}, nil
}

func (f *fakeHandler) PostReceive(_ context.Context, project, pushID string, updates []Update) (Response, error) {
	f.record(PostReceive, project, pushID, updates)
	return Response{Allow: true, Messages: []string{"post: delivered"}}, nil
}

func shortSocket(t *testing.T) string {
	// Unix socket paths are limited to ~100 bytes; t.TempDir() can be longer.
	dir, err := os.MkdirTemp("", "mg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "h.sock")
}

func startServer(t *testing.T, h Handler) *Server {
	t.Helper()
	s, err := Listen(shortSocket(t), h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func envFunc(env []string) func(string) string {
	return func(k string) string {
		for _, kv := range env {
			if key, v, _ := strings.Cut(kv, "="); key == k {
				return v
			}
		}
		return ""
	}
}

const (
	zero = "0000000000000000000000000000000000000000"
	oidA = "1111111111111111111111111111111111111111"
)

func TestHookTalksToService(t *testing.T) {
	h := &fakeHandler{allow: true}
	s := startServer(t, h)
	env, done := s.NewPush("foo")
	defer done()

	var stderr bytes.Buffer
	stdin := strings.NewReader(zero + " " + oidA + " refs/heads/main\n")
	if code := RunHook(t.Context(), PreReceive, stdin, &stderr, envFunc(env)); code != 0 {
		t.Fatalf("pre-receive exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "pre: foo") {
		t.Fatalf("service message is not printed: %q", stderr.String())
	}
	stdin = strings.NewReader(zero + " " + oidA + " refs/heads/main\n")
	if code := RunHook(t.Context(), PostReceive, stdin, &stderr, envFunc(env)); code != 0 {
		t.Fatalf("post-receive exit %d", code)
	}
	if !strings.Contains(stderr.String(), "post: delivered") {
		t.Fatalf("post message is not printed: %q", stderr.String())
	}

	if len(h.calls) != 2 {
		t.Fatalf("calls = %+v", h.calls)
	}
	pre := h.calls[0]
	if pre.Project != "foo" || len(pre.Updates) != 1 || pre.Updates[0].Kind != KindCreate || pre.Updates[0].Ref != "refs/heads/main" {
		t.Fatalf("pre-receive request = %+v", pre)
	}
}

func TestHookRejectsWhenServiceSaysNo(t *testing.T) {
	s := startServer(t, &fakeHandler{allow: false})
	env, done := s.NewPush("foo")
	defer done()
	var stderr bytes.Buffer
	stdin := strings.NewReader(zero + " " + oidA + " refs/heads/main\n")
	if code := RunHook(t.Context(), PreReceive, stdin, &stderr, envFunc(env)); code == 0 {
		t.Fatal("push must be rejected")
	}
}

func TestHookWithUnknownPushOrWithoutService(t *testing.T) {
	h := &fakeHandler{allow: true}
	s := startServer(t, h)
	env, done := s.NewPush("foo")
	done() // the push is no longer registered

	var stderr bytes.Buffer
	stdin := strings.NewReader(zero + " " + oidA + " refs/heads/main\n")
	if code := RunHook(t.Context(), PreReceive, stdin, &stderr, envFunc(env)); code == 0 {
		t.Fatal("unknown push must be rejected")
	}
	if len(h.calls) != 0 {
		t.Fatal("handler must not be called for an unknown push")
	}

	stderr.Reset()
	if code := RunHook(t.Context(), PreReceive, strings.NewReader(""), &stderr, func(string) string { return "" }); code == 0 {
		t.Fatal("push outside metagit must be rejected")
	}
	if !strings.Contains(stderr.String(), "only through the metagit service") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestClassify(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	r := &gitcmd.Runner{}
	run := func(args ...string) string {
		t.Helper()
		env := []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}
		out, err := r.Output(ctx, gitcmd.Opts{Dir: dir, Env: env}, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "a")
	a := run("rev-parse", "HEAD")
	run("commit", "-q", "--allow-empty", "-m", "b")
	b := run("rev-parse", "HEAD")
	run("checkout", "-q", "--orphan", "other")
	run("commit", "-q", "--allow-empty", "-m", "c")
	c := run("rev-parse", "HEAD")

	repoRunner := &gitcmd.Runner{Env: append(gitcmd.CleanEnv(), "GIT_DIR="+filepath.Join(dir, ".git"))}
	cases := []struct {
		u    Update
		want Kind
	}{
		{Update{Old: zero, New: a, Ref: "refs/heads/x"}, KindCreate},
		{Update{Old: a, New: zero, Ref: "refs/heads/x"}, KindDelete},
		{Update{Old: a, New: b, Ref: "refs/heads/x"}, KindFastForward},
		{Update{Old: b, New: a, Ref: "refs/heads/x"}, KindNonFastForward},
		{Update{Old: b, New: c, Ref: "refs/heads/x"}, KindNonFastForward},
		{Update{Old: a, New: b, Ref: "refs/tags/v1"}, KindTagMove},
	}
	for _, tc := range cases {
		got, err := Classify(ctx, repoRunner, tc.u)
		if err != nil || got != tc.want {
			t.Errorf("%+v: got %q %v, want %q", tc.u, got, err, tc.want)
		}
	}
	destructive := []Kind{KindNonFastForward, KindDelete, KindTagMove}
	for _, k := range []Kind{KindCreate, KindFastForward, KindNonFastForward, KindDelete, KindTagMove} {
		if k.Destructive() != slices.Contains(destructive, k) {
			t.Errorf("%s.Destructive() is wrong", k)
		}
	}
}

func TestParseUpdates(t *testing.T) {
	in := zero + " " + oidA + " refs/heads/main\n\n" + oidA + " " + zero + " refs/tags/v1\n"
	updates, err := ParseUpdates(strings.NewReader(in))
	if err != nil || len(updates) != 2 || updates[1].Ref != "refs/tags/v1" {
		t.Fatalf("updates = %+v, %v", updates, err)
	}
	if _, err := ParseUpdates(strings.NewReader("bad line\n")); err == nil {
		t.Fatal("expected error")
	}
}

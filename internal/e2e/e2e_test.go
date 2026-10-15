// Package e2e runs the metagit binary as a separate process and uses it the
// way the owner does: the CLI, the web API and git.
package e2e

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/gitssh"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/testutil"
)

// sshHost is a git hosting reachable over SSH.
type sshHost struct {
	t      *testing.T
	repos  *repo.Manager
	key    ssh.Signer
	hostKy ssh.Signer
	addr   string
	srv    *gitssh.Server
}

type noPushes struct{}

func (noPushes) NewPush(string) ([]string, func()) { return nil, func() {} }

func newSSHHost(t *testing.T, clientKey ssh.Signer) *sshHost {
	dir := t.TempDir()
	h := &sshHost{t: t, key: clientKey,
		repos: &repo.Manager{ReposDir: dir, HooksDir: filepath.Join(dir, "no-hooks"), Git: &gitcmd.Runner{}}}
	if err := h.repos.Create(context.Background(), "foo"); err != nil {
		t.Fatal(err)
	}
	var err error
	if h.hostKy, err = gitssh.LoadOrCreateHostKey(filepath.Join(dir, "host_key")); err != nil {
		t.Fatal(err)
	}
	h.start("127.0.0.1:0")
	t.Cleanup(h.stop)
	return h
}

func (h *sshHost) start(addr string) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		h.t.Fatal(err)
	}
	h.addr = l.Addr().String()
	h.srv = &gitssh.Server{
		ReposDir: h.repos.ReposDir,
		HostKey:  h.hostKy,
		CheckKey: func(_ context.Context, k ssh.PublicKey) bool {
			return bytes.Equal(k.Marshal(), h.key.PublicKey().Marshal())
		},
		ProjectExists: func(_ context.Context, name string) bool { return h.repos.Exists(name) },
		Pushes:        noPushes{},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go h.srv.Serve(l)
}

func (h *sshHost) stop() {
	if h.srv != nil {
		h.srv.Close()
		h.srv = nil
	}
}

func (h *sshHost) url() string { return "ssh://git@" + h.addr + "/foo.git" }

func (h *sshHost) ref(ref string) string { return testutil.RefOf(h.t, h.repos.Path("foo"), ref) }

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

type web struct {
	t    *testing.T
	base string
	c    *http.Client
}

func (w *web) do(method, path string, body any) (int, []byte) {
	w.t.Helper()
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, w.base+path, r)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := w.c.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

func (w *web) must(method, path string, body any, out any) {
	w.t.Helper()
	status, data := w.do(method, path, body)
	if status/100 != 2 {
		w.t.Fatalf("%s %s: %d %s", method, path, status, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			w.t.Fatalf("%s %s: %v: %s", method, path, err, data)
		}
	}
}

type target struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	LastError string `json:"last_error"`
}

func (w *web) targets() map[string]target {
	var list []target
	w.must("GET", "/api/projects/foo/targets", nil, &list)
	m := map[string]target{}
	for _, t := range list {
		m[t.Name] = t
	}
	return m
}

func (w *web) waitTarget(name, what string, cond func(target) bool) target {
	w.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		tg := w.targets()[name]
		if cond(tg) {
			return tg
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("target %s: timeout waiting for %s: %+v", name, what, tg)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestEndToEnd(t *testing.T) {
	bin := testutil.Binary(t)
	dataDir := filepath.Join(t.TempDir(), "data")

	// The owner is created with the CLI on the server.
	cmd := exec.Command(bin, "owner", "create", "--data-dir", dataDir, "--name", "eugene")
	cmd.Stdin = strings.NewReader("password-1\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("owner create: %v\n%s", err, out)
	}

	httpAddr, sshAddr := freeAddr(t), freeAddr(t)
	var logs bytes.Buffer
	serve := exec.Command(bin, "serve", "--data-dir", dataDir, "--http-addr", httpAddr, "--ssh-addr", sshAddr,
		"--push-status-timeout", "5s")
	serve.Stdout, serve.Stderr = &logs, &logs
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- serve.Wait() }()
	t.Cleanup(func() {
		_ = serve.Process.Kill()
		if t.Failed() {
			t.Logf("metagit log:\n%s", logs.String())
		}
	})

	jar, _ := cookiejar.New(nil)
	w := &web{t: t, base: "http://" + httpAddr, c: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	for start := time.Now(); ; {
		if res, err := http.Get(w.base + "/api/session"); err == nil {
			res.Body.Close()
			break
		}
		if time.Since(start) > 20*time.Second {
			t.Fatalf("metagit did not start:\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The owner logs in, creates a git token and a project.
	w.must("POST", "/api/login", map[string]string{"name": "eugene", "password": "password-1"}, nil)
	var token struct {
		Value string `json:"value"`
	}
	w.must("POST", "/api/tokens", map[string]string{"name": "laptop"}, &token)
	w.must("POST", "/api/projects", map[string]string{"name": "foo"}, nil)
	w.must("POST", "/api/projects/foo/protected", map[string]string{"pattern": "refs/heads/main"}, nil)

	// Two hostings, both over SSH with the same deploy key.
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	signer, _ := ssh.NewSignerFromKey(priv)
	keyPEM := string(pem.EncodeToMemory(block))
	h1, h2 := newSSHHost(t, signer), newSSHHost(t, signer)
	for name, h := range map[string]*sshHost{"hosting1": h1, "hosting2": h2} {
		w.must("POST", "/api/projects/foo/targets", map[string]any{
			"name": name, "url": h.url(), "auth_kind": "ssh", "secret": keyPEM, "enabled": true,
		}, nil)
	}

	u, _ := url.Parse(w.base + "/foo.git")
	u.User = url.UserPassword("eugene", token.Value)
	remote := u.String()
	push := func(work string, args ...string) string {
		t.Helper()
		out, err := testutil.GitErr(work, nil, append([]string{"push", remote}, args...)...)
		if err != nil {
			t.Fatalf("push %v: %v\n%s", args, err, out)
		}
		return out
	}

	// Push: both hostings receive main and feature.
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "c")
	out := push(work, "main", "main:feature")
	for _, want := range []string{"hosting1: доставлено", "hosting2: доставлено"} {
		if !strings.Contains(out, want) {
			t.Fatalf("push output has no %q:\n%s", want, out)
		}
	}
	if h1.ref("refs/heads/main") != c || h2.ref("refs/heads/feature") != c {
		t.Fatal("hostings did not receive the push")
	}

	// Force-push to an unprotected branch: archived in metagit, delivered.
	testutil.Git(t, work, "checkout", "-q", "--orphan", "rewrite")
	x := testutil.Commit(t, work, "x")
	push(work, "--force", "rewrite:feature")
	if h1.ref("refs/heads/feature") != x || h2.ref("refs/heads/feature") != x {
		t.Fatal("force-push is not delivered")
	}
	var archive []struct {
		Source  string `json:"source"`
		OrigRef string `json:"orig_ref"`
		Object  string `json:"object"`
	}
	w.must("GET", "/api/projects/foo/archive", nil, &archive)
	if len(archive) != 1 || archive[0].Source != "push" || archive[0].Object != c {
		t.Fatalf("archive = %+v", archive)
	}
	// Force-push to the protected branch is rejected.
	if out, err := testutil.GitErr(work, nil, "push", "--force", remote, "rewrite:main"); err == nil || !strings.Contains(out, "защищен") {
		t.Fatalf("protected main: %v\n%s", err, out)
	}

	// Divergence: somebody pushes to hosting1 in bypass of metagit.
	other := filepath.Join(t.TempDir(), "other")
	testutil.Git(t, t.TempDir(), "clone", "-q", h1.repos.Path("foo"), other)
	eCommit := testutil.Commit(t, other, "e on hosting1")
	testutil.Git(t, other, "push", "-q", h1.repos.Path("foo"), "main")
	testutil.Git(t, work, "checkout", "-q", "main")
	d := testutil.Commit(t, work, "d")
	out = push(work, "main")
	if !strings.Contains(out, "hosting1: доставлено; расхождение") || !strings.Contains(out, "hosting2: доставлено") {
		t.Fatalf("push output:\n%s", out)
	}
	if h1.ref("refs/heads/main") != d {
		t.Fatal("diverged main is not overwritten")
	}
	w.must("GET", "/api/projects/foo/archive", nil, &archive)
	var saved bool
	for _, a := range archive {
		if a.Source == "target" && a.Object == eCommit {
			saved = true
		}
	}
	if !saved {
		t.Fatalf("the commit made on hosting1 is not archived: %+v", archive)
	}

	// hosting2 goes down: the push still succeeds, the target reports an error.
	addr2 := h2.addr
	h2.stop()
	f := testutil.Commit(t, work, "f")
	out = push(work, "main")
	if !strings.Contains(out, "hosting1: доставлено") || !strings.Contains(out, "hosting2: ошибка") {
		t.Fatalf("push output with hosting2 down:\n%s", out)
	}
	w.waitTarget("hosting2", "error", func(tg target) bool { return tg.State == "error" && tg.LastError != "" })

	// hosting2 is back: a manual delivery catches up.
	h2.start(addr2)
	tg := w.targets()["hosting2"]
	w.must("POST", fmt.Sprintf("/api/projects/foo/targets/%d/sync", tg.ID), nil, nil)
	w.waitTarget("hosting2", "synced", func(tg target) bool { return tg.State == "synced" })
	if h2.ref("refs/heads/main") != f || h1.ref("refs/heads/main") != f {
		t.Fatal("hostings did not catch up")
	}

	// Graceful stop.
	if err := serve.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("serve exited with %v\n%s", err, logs.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not stop")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "metagit.db")); err != nil {
		t.Fatal(err)
	}
}

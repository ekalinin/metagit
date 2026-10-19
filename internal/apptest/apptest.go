// Package apptest starts a real metagit instance for integration tests.
package apptest

import (
	"bytes"
	"cmp"
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
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/app"
	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
	"github.com/ekalinin/metagit/internal/testutil/fakehost"
)

// Owner credentials of test instances.
const (
	OwnerName     = "eugene"
	OwnerPassword = "password-1"
)

// Options configure a test instance.
type Options struct {
	// DataDir reuses the data of a stopped instance.
	DataDir           string
	PushStatusTimeout time.Duration
	RetryMin          time.Duration
	RetryMax          time.Duration
	// NoOwner starts an instance where the owner is not created yet.
	NoOwner bool
	// TrustedProxies are reverse proxies whose X-Forwarded-For is used.
	TrustedProxies []netip.Prefix
}

// Env is a running instance.
type Env struct {
	t          testing.TB
	App        *app.App
	DataDir    string
	BaseURL    string
	SSHAddr    string
	Token      string
	SSHKeyPath string

	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

// Start runs metagit with an owner, a git token and an SSH key.
func Start(t testing.TB, o Options) *Env {
	t.Helper()
	dataDir := cmp.Or(o.DataDir, filepath.Join(t.TempDir(), "data"))
	httpL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sshL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DataDir:               dataDir,
		HTTPAddr:              httpL.Addr().String(),
		SSHAddr:               sshL.Addr().String(),
		BaseURL:               "http://" + httpL.Addr().String(),
		SSHURL:                "ssh://git@" + sshL.Addr().String(),
		TrustedProxies:        o.TrustedProxies,
		PushStatusTimeout:     cmp.Or(o.PushStatusTimeout, 5*time.Second),
		MaxParallelDeliveries: 4,
		GitTimeout:            time.Minute,
	}
	var logOut io.Writer = io.Discard
	if os.Getenv("METAGIT_TEST_LOG") != "" {
		logOut = os.Stderr
	}
	ctx := context.Background()
	a, err := app.New(ctx, app.Options{
		Config:             cfg,
		Binary:             testutil.Binary(t),
		Log:                slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelDebug})),
		WebFS:              fstest.MapFS{"index.html": {Data: []byte(`<div id="root"></div>`)}},
		RetryMin:           cmp.Or(o.RetryMin, 200*time.Millisecond),
		RetryMax:           cmp.Or(o.RetryMax, 2*time.Second),
		ExtraGitEnv:        fakehost.GitEnv,
		TransferWorkFactor: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{t: t, App: a, DataDir: dataDir, BaseURL: cfg.BaseURL, SSHAddr: sshL.Addr().String()}

	if !o.NoOwner {
		if _, err := a.Auth.CreateOwner(ctx, OwnerName, OwnerPassword); err != nil {
			t.Fatal(err)
		}
	}
	if e.Token, _, err = a.Auth.CreateToken(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	e.SSHKeyPath = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(e.SSHKeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	if _, err := a.Auth.AddSSHKey(ctx, "test", string(ssh.MarshalAuthorizedKey(signer.PublicKey()))); err != nil {
		t.Fatal(err)
	}

	serveCtx, cancel := context.WithCancel(ctx)
	e.cancel, e.done = cancel, make(chan struct{})
	go func() {
		defer close(e.done)
		if err := a.Serve(serveCtx, httpL, sshL); err != nil {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(e.Stop)
	return e
}

// Stop shuts the instance down. It is safe to call twice.
func (e *Env) Stop() {
	if e.stopped {
		return
	}
	e.stopped = true
	e.cancel()
	<-e.done
	e.App.Close()
}

// GitURL is the HTTP URL of a project with the token inside.
func (e *Env) GitURL(project string) string {
	u, _ := url.Parse(e.BaseURL + "/" + project + ".git")
	u.User = url.UserPassword(OwnerName, e.Token)
	return u.String()
}

// SSHURL is the SSH URL of a project.
func (e *Env) SSHURL(project string) string {
	return "ssh://git@" + e.SSHAddr + "/" + project + ".git"
}

// SSHEnv makes git use the owner's key for SSH.
func (e *Env) SSHEnv() []string {
	return []string{"GIT_SSH_COMMAND=ssh -F /dev/null -i " + e.SSHKeyPath +
		" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR"}
}

// RepoPath is the metagit repository of a project.
func (e *Env) RepoPath(project string) string { return e.App.Repos.Path(project) }

// CreateProject creates a project or fails the test.
func (e *Env) CreateProject(name string) *store.Project {
	e.t.Helper()
	p, err := e.App.Core.CreateProject(context.Background(), name)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

// AddTarget adds an HTTPS target with a token.
func (e *Env) AddTarget(project, name, url, token string, enabled bool) *store.Target {
	e.t.Helper()
	t, err := e.App.Core.AddTarget(context.Background(), project, core.TargetInput{
		Name: name, URL: url, AuthKind: store.AuthHTTPS, Username: "x", Secret: &token, Enabled: enabled,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return t
}

// Push runs "git push <metagit url> args..." in work and fails the test on
// error. It returns the combined output.
func (e *Env) Push(work, project string, args ...string) string {
	e.t.Helper()
	out, err := e.PushErr(work, project, args...)
	if err != nil {
		e.t.Fatalf("push %v: %v\n%s", args, err, out)
	}
	return out
}

// PushErr is Push that returns the error.
func (e *Env) PushErr(work, project string, args ...string) (string, error) {
	return testutil.GitErr(work, nil, append([]string{"push", e.GitURL(project)}, args...)...)
}

// Target reads a target from the store.
func (e *Env) Target(id int64) *store.Target {
	e.t.Helper()
	t, err := e.App.Store.TargetByID(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return t
}

// WaitTarget polls a target until cond holds.
func (e *Env) WaitTarget(id int64, what string, cond func(*store.Target) bool) *store.Target {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		t := e.Target(id)
		if cond(t) {
			return t
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("target %s: timeout waiting for %s; state %q, error %q", t.Name, what, t.State, t.LastError)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// WaitSynced waits until the target is synced.
func (e *Env) WaitSynced(id int64) *store.Target {
	e.t.Helper()
	return e.WaitTarget(id, "synced", func(t *store.Target) bool { return t.State == store.StateSynced })
}

// Events returns events of a project, newest first.
func (e *Env) Events(project string) []store.Event {
	e.t.Helper()
	events, err := e.App.Core.Events(context.Background(), project, 1000)
	if err != nil {
		e.t.Fatal(err)
	}
	return events
}

// Archive returns archive records of a project, newest first.
func (e *Env) Archive(project string) []store.ArchiveRef {
	e.t.Helper()
	list, err := e.App.Core.ArchiveRefs(context.Background(), project)
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

// Client is a web client with a cookie jar.
type Client struct {
	t    testing.TB
	base string
	http *http.Client
}

// Client returns a web client that is not logged in.
func (e *Env) Client() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{t: e.t, base: e.BaseURL, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

// LoggedIn returns a web client with an owner session.
func (e *Env) LoggedIn() *Client {
	e.t.Helper()
	c := e.Client()
	if status, body := c.Do("POST", "/api/login", map[string]string{"name": OwnerName, "password": OwnerPassword}); status != 200 {
		e.t.Fatalf("login: %d %s", status, body)
	}
	return c
}

// Do sends a JSON request and returns the status and the body.
func (c *Client) Do(method, path string, body any) (int, string) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

// Raw sends a request with the given content type.
func (c *Client) Raw(method, path, contentType, body string) (*http.Response, string) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

// JSON decodes a response body or fails the test.
func JSON[T any](t testing.TB, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

// Must fails the test on error.
func Must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Sprint is a short helper for messages.
func Sprint(v ...any) string { return fmt.Sprint(v...) }

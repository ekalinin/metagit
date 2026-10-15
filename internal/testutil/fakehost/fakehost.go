// Package fakehost is a git hosting for tests: an HTTPS server with
// "git http-backend" over temporary bare repositories and token checks.
package fakehost

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ekalinin/metagit/internal/githttp"
	"github.com/ekalinin/metagit/internal/testutil"
)

// GitEnv must be given to git clients of the host: the certificate of the
// test server is self-signed.
var GitEnv = []string{"GIT_SSL_NO_VERIFY=1"}

// Host is a fake git hosting.
type Host struct {
	Root  string
	Token string

	srv      *httptest.Server
	down     atomic.Bool
	delay    atomic.Int64
	requests atomic.Int64
}

type noPushes struct{}

func (noPushes) NewPush(string) ([]string, func()) { return nil, func() {} }

// New starts a host that accepts any user name with the given token.
func New(t testing.TB, token string) *Host {
	t.Helper()
	h := &Host{Root: t.TempDir(), Token: token}
	gh := &githttp.Handler{
		ReposDir:   h.Root,
		CheckToken: func(_ context.Context, tok string) bool { return tok == h.Token },
		ProjectExists: func(_ context.Context, name string) bool {
			_, err := os.Stat(h.Path(name))
			return err == nil
		},
		Owner:  func(context.Context) string { return "host" },
		Pushes: noPushes{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.requests.Add(1)
		if d := time.Duration(h.delay.Load()); d > 0 {
			time.Sleep(d)
		}
		if h.down.Load() {
			http.Error(w, "hosting is down", http.StatusServiceUnavailable)
			return
		}
		gh.ServeHTTP(w, r)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// URL returns the HTTPS URL of a repository (without credentials).
func (h *Host) URL(name string) string { return h.srv.URL + "/" + name + ".git" }

// Path returns the directory of a repository.
func (h *Host) Path(name string) string { return filepath.Join(h.Root, name+".git") }

// CreateRepo creates an empty bare repository.
func (h *Host) CreateRepo(t testing.TB, name string) string {
	t.Helper()
	testutil.Git(t, h.Root, "init", "-q", "--bare", "-b", "main", h.Path(name))
	return h.Path(name)
}

// SetDown makes the host answer 503 to every request.
func (h *Host) SetDown(down bool) { h.down.Store(down) }

// SetDelay delays every request.
func (h *Host) SetDelay(d time.Duration) { h.delay.Store(int64(d)) }

// Requests returns the number of requests the host received.
func (h *Host) Requests() int64 { return h.requests.Load() }

// Ref returns the object of ref in a repository of the host, or "".
func (h *Host) Ref(t testing.TB, name, ref string) string {
	t.Helper()
	return testutil.RefOf(t, h.Path(name), ref)
}

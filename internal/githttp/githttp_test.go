package githttp

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/auth"
	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
)

type noPushes struct{}

func (noPushes) NewPush(string) ([]string, func()) { return nil, func() {} }

type env struct {
	srv   *httptest.Server
	auth  *auth.Service
	repos *repo.Manager
	token string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(t.Context(), filepath.Join(dir, "metagit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := auth.New(st)
	token, _, err := a.CreateToken(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	repos := &repo.Manager{ReposDir: filepath.Join(dir, "repos"), HooksDir: filepath.Join(dir, "hooks"), Git: &gitcmd.Runner{}}
	if err := repos.Create(t.Context(), "foo"); err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		ReposDir:      repos.ReposDir,
		CheckToken:    a.CheckToken,
		ProjectExists: func(_ context.Context, name string) bool { return repos.Exists(name) },
		Owner:         func(context.Context) string { return "eugene" },
		Pushes:        noPushes{},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{srv: srv, auth: a, repos: repos, token: token}
}

func (e *env) url(project, token string) string {
	u, _ := url.Parse(e.srv.URL + "/" + project + ".git")
	if token != "" {
		u.User = url.UserPassword("eugene", token)
	}
	return u.String()
}

func TestCloneAndPushWithToken(t *testing.T) {
	e := setup(t)
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "first")
	testutil.Git(t, work, "push", "-q", e.url("foo", e.token), "main")

	if got := testutil.RefOf(t, e.repos.Path("foo"), "refs/heads/main"); got != c {
		t.Fatalf("main in metagit = %q, want %q", got, c)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	testutil.Git(t, t.TempDir(), "clone", "-q", e.url("foo", e.token), clone)
	if got := testutil.RefOf(t, clone, "HEAD"); got != c {
		t.Fatalf("clone HEAD = %q", got)
	}
	// Protocol v2 is used by default for fetch.
	testutil.Git(t, clone, "-c", "protocol.version=2", "fetch", "-q", "origin")
}

func TestRequestsWithoutValidToken(t *testing.T) {
	e := setup(t)
	for name, token := range map[string]string{"no token": "", "wrong token": "mgt_wrong"} {
		t.Run(name, func(t *testing.T) {
			res, err := http.Get(e.url("foo", token) + "/info/refs?service=git-upload-pack")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusUnauthorized || strings.Contains(string(body), "git-upload-pack") {
				t.Fatalf("status %d, body %q", res.StatusCode, body)
			}
			if !strings.Contains(res.Header.Get("WWW-Authenticate"), "Basic") {
				t.Fatal("no WWW-Authenticate header")
			}
		})
	}

	out, err := testutil.GitErr(t.TempDir(), nil, "clone", e.url("foo", "mgt_wrong"), "x")
	if err == nil || !strings.Contains(out, "Authentication failed") {
		t.Fatalf("clone with wrong token: %v\n%s", err, out)
	}
}

func TestRevokedToken(t *testing.T) {
	e := setup(t)
	tokens, _ := e.auth.ListTokens(t.Context())
	if err := e.auth.RevokeToken(t.Context(), tokens[0].ID); err != nil {
		t.Fatal(err)
	}
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "first")
	out, err := testutil.GitErr(work, nil, "push", e.url("foo", e.token), "main")
	if err == nil || !strings.Contains(out, "Authentication failed") {
		t.Fatalf("push with revoked token: %v\n%s", err, out)
	}
}

func TestUnknownProject(t *testing.T) {
	e := setup(t)
	out, err := testutil.GitErr(t.TempDir(), nil, "clone", e.url("nope", e.token), "x")
	if err == nil || !strings.Contains(out, "not found") {
		t.Fatalf("clone of unknown project: %v\n%s", err, out)
	}
	res, err := http.Get(e.url("Bad..Name", e.token) + "/info/refs")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid name status %d", res.StatusCode)
	}
}

func TestProjectFromPath(t *testing.T) {
	cases := map[string]string{
		"/foo.git/info/refs":       "foo",
		"/foo.git":                 "foo",
		"/a.b.git/git-upload-pack": "a.b",
	}
	for path, want := range cases {
		if got, ok := ProjectFromPath(path); !ok || got != want {
			t.Errorf("%s: %q %v", path, got, ok)
		}
	}
	for _, path := range []string{"/", "/api/projects", "/.git/x", "/projects/foo"} {
		if _, ok := ProjectFromPath(path); ok {
			t.Errorf("%s must not be a git path", path)
		}
	}
}

func TestLargePushUsesChunkedBody(t *testing.T) {
	e := setup(t)
	work := testutil.InitWork(t)
	data := make([]byte, 3<<20)
	_, _ = rand.Read(data)
	if err := os.WriteFile(filepath.Join(work, "blob.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, work, "add", "blob.bin")
	c := testutil.Commit(t, work, "large")
	testutil.Git(t, work, "-c", "http.postBuffer=65536", "push", "-q", e.url("foo", e.token), "main")
	if got := testutil.RefOf(t, e.repos.Path("foo"), "refs/heads/main"); got != c {
		t.Fatalf("large push did not arrive: %q", got)
	}
}

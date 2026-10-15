// Package githttp serves git smart HTTP through "git http-backend".
package githttp

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"strings"

	"github.com/ekalinin/metagit/internal/repo"
)

// Pushes registers git invocations so that hooks can reach the service.
type Pushes interface {
	NewPush(project string) (env []string, done func())
}

// Handler serves /<project>.git/... paths.
type Handler struct {
	// ReposDir contains <project>.git repositories.
	ReposDir string
	// Git is the git binary, "git" when empty.
	Git string
	// CheckToken validates the token from HTTP Basic auth.
	CheckToken func(ctx context.Context, token string) bool
	// ProjectExists reports whether the project exists.
	ProjectExists func(ctx context.Context, name string) bool
	// Owner returns the owner name passed to git as REMOTE_USER.
	Owner  func(ctx context.Context) string
	Pushes Pushes
	Log    *slog.Logger
	// TmpDir keeps chunked request bodies; os.TempDir() when empty.
	TmpDir string
}

// ProjectFromPath returns the project name of a git path like
// "/foo.git/info/refs", or false when the path is not a git path.
func ProjectFromPath(path string) (string, bool) {
	first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	name, ok := strings.CutSuffix(first, ".git")
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, ok := ProjectFromPath(r.URL.Path)
	if !ok || repo.ValidateName(name) != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}
	_, token, hasAuth := r.BasicAuth()
	if !hasAuth || !h.CheckToken(r.Context(), token) {
		w.Header().Set("WWW-Authenticate", `Basic realm="metagit", charset="UTF-8"`)
		http.Error(w, "authentication required: use an access token of metagit as the password", http.StatusUnauthorized)
		return
	}
	if !h.ProjectExists(r.Context(), name) {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	if r.ContentLength < 0 && r.Body != nil && r.Body != http.NoBody {
		// net/http/cgi refuses chunked bodies, and git sends large pushes
		// chunked. Spool the body to a file to give it a length.
		cleanup, err := h.spoolBody(r)
		if err != nil {
			h.Log.Error("cannot spool request body", "err", err)
			http.Error(w, "cannot read request body", http.StatusBadRequest)
			return
		}
		defer cleanup()
	}

	pushEnv, done := h.Pushes.NewPush(name)
	defer done()
	git, err := exec.LookPath(cmp.Or(h.Git, "git"))
	if err != nil {
		h.Log.Error("git is not found", "err", err)
		http.Error(w, "git is not available on the server", http.StatusInternalServerError)
		return
	}
	env := []string{
		"GIT_PROJECT_ROOT=" + h.ReposDir,
		"GIT_HTTP_EXPORT_ALL=1",
		"REMOTE_USER=" + h.Owner(r.Context()),
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
	}
	backend := &cgi.Handler{
		Path:   git,
		Args:   []string{"http-backend"},
		Dir:    h.ReposDir,
		Root:   "/",
		Env:    append(env, pushEnv...),
		Logger: slog.NewLogLogger(h.Log.Handler(), slog.LevelError),
		Stderr: logWriter{h.Log},
	}
	backend.ServeHTTP(w, r)
}

func (h *Handler) spoolBody(r *http.Request) (func(), error) {
	f, err := os.CreateTemp(h.TmpDir, "metagit-body-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		f.Close()
		os.Remove(f.Name())
	}
	n, err := io.Copy(f, r.Body)
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		cleanup()
		return nil, err
	}
	r.Body.Close()
	r.Body = f
	r.ContentLength = n
	r.TransferEncoding = nil
	return cleanup, nil
}

type logWriter struct{ log *slog.Logger }

func (l logWriter) Write(p []byte) (int, error) {
	l.log.Warn("git http-backend", "stderr", strings.TrimSpace(string(p)))
	return len(p), nil
}

// Package hooks connects git hooks of metagit repositories with the running
// service through an HTTP API on a unix socket.
package hooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Environment variables the service passes to git and git passes to hooks.
const (
	EnvSocket  = "METAGIT_SOCKET"
	EnvProject = "METAGIT_PROJECT"
	EnvPushID  = "METAGIT_PUSH_ID"
)

// Hook names.
const (
	PreReceive  = "pre-receive"
	PostReceive = "post-receive"
)

// Kind classifies a ref update.
type Kind string

// Kinds of ref updates.
const (
	KindCreate         Kind = "create"
	KindFastForward    Kind = "fast-forward"
	KindNonFastForward Kind = "non-fast-forward"
	KindDelete         Kind = "delete"
	KindTagMove        Kind = "tag-move"
)

// Destructive reports whether the update loses the previous value of the ref.
func (k Kind) Destructive() bool {
	return k == KindNonFastForward || k == KindDelete || k == KindTagMove
}

// Update is one ref update of a push.
type Update struct {
	Old  string `json:"old"`
	New  string `json:"new"`
	Ref  string `json:"ref"`
	Kind Kind   `json:"kind,omitempty"`
}

// Request is sent by a hook to the service.
type Request struct {
	Hook    string   `json:"hook"`
	PushID  string   `json:"push_id"`
	Project string   `json:"project"`
	Updates []Update `json:"updates"`
}

// Response tells the hook what to print and whether to accept the push.
type Response struct {
	Allow    bool     `json:"allow"`
	Messages []string `json:"messages,omitzero"`
}

// Handler decides on pushes. It is implemented by the service.
type Handler interface {
	PreReceive(ctx context.Context, project, pushID string, updates []Update) (Response, error)
	PostReceive(ctx context.Context, project, pushID string, updates []Update) (Response, error)
}

// Server is the service side of the hook API.
type Server struct {
	handler  Handler
	path     string
	listener net.Listener
	srv      *http.Server
	log      *slog.Logger

	mu     sync.Mutex
	pushes map[string]string // push id -> project
}

// Listen creates the unix socket at path (removing a stale one) and starts
// serving requests.
func Listen(path string, handler Handler, log *slog.Logger) (*Server, error) {
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	s := &Server{handler: handler, path: path, listener: l, log: log, pushes: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook", s.serveHook)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := s.srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("hook API stopped", "err", err)
		}
	}()
	return s, nil
}

// Path returns the socket path.
func (s *Server) Path() string { return s.path }

// Close stops the server and removes the socket.
func (s *Server) Close() error {
	err := s.srv.Close()
	_ = os.Remove(s.path)
	return err
}

// NewPush registers a git invocation for a project. The returned environment
// must be given to git; done must be called when git exits.
func (s *Server) NewPush(project string) (env []string, done func()) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	s.mu.Lock()
	s.pushes[id] = project
	s.mu.Unlock()
	env = []string{EnvSocket + "=" + s.path, EnvProject + "=" + project, EnvPushID + "=" + id}
	return env, func() {
		s.mu.Lock()
		delete(s.pushes, id)
		s.mu.Unlock()
	}
}

func (s *Server) serveHook(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	project, ok := s.pushes[req.PushID]
	s.mu.Unlock()
	if !ok || project != req.Project {
		http.Error(w, "unknown push", http.StatusForbidden)
		return
	}
	var resp Response
	var err error
	switch req.Hook {
	case PreReceive:
		resp, err = s.handler.PreReceive(r.Context(), req.Project, req.PushID, req.Updates)
	case PostReceive:
		resp, err = s.handler.PostReceive(r.Context(), req.Project, req.PushID, req.Updates)
	default:
		http.Error(w, "unknown hook "+req.Hook, http.StatusBadRequest)
		return
	}
	if err != nil {
		s.log.Error("hook failed", "hook", req.Hook, "project", req.Project, "err", err)
		resp = Response{Allow: false, Messages: []string{"metagit: internal error: " + strings.TrimSpace(err.Error())}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

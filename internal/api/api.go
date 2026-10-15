// Package api is the JSON API of the web interface.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ekalinin/metagit/internal/auth"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/store"
)

// Transfer exports and imports settings.
type Transfer interface {
	Export(ctx context.Context, passphrase string) ([]byte, error)
	Import(ctx context.Context, data []byte, passphrase string) ([]string, error)
}

// API serves /api/.
type API struct {
	Core     *core.Service
	Auth     *auth.Service
	Transfer Transfer
	// BaseURL and SSHURL are used to show clone addresses.
	BaseURL string
	SSHURL  string
	// SecureCookies sets the Secure flag on the session cookie.
	SecureCookies bool
	Log           *slog.Logger
}

// Handler returns the HTTP handler of the API.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	public := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.Handle(pattern, a.wrap(h, false))
	}
	private := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.Handle(pattern, a.wrap(h, true))
	}
	public("GET /api/session", a.session)
	public("POST /api/login", a.login)
	public("POST /api/logout", a.logout)

	private("GET /api/projects", a.listProjects)
	private("POST /api/projects", a.createProject)
	private("GET /api/projects/{name}", a.getProject)
	private("POST /api/projects/{name}/delete", a.deleteProject)
	private("GET /api/projects/{name}/targets", a.listTargets)
	private("POST /api/projects/{name}/targets", a.createTarget)
	private("PUT /api/projects/{name}/targets/{id}", a.updateTarget)
	private("DELETE /api/projects/{name}/targets/{id}", a.deleteTarget)
	private("POST /api/projects/{name}/targets/{id}/sync", a.syncTarget)
	private("GET /api/projects/{name}/protected", a.listProtected)
	private("POST /api/projects/{name}/protected", a.addProtected)
	private("DELETE /api/projects/{name}/protected/{id}", a.deleteProtected)
	private("GET /api/projects/{name}/events", a.listEvents)
	private("GET /api/projects/{name}/archive", a.listArchive)
	private("GET /api/tokens", a.listTokens)
	private("POST /api/tokens", a.createToken)
	private("DELETE /api/tokens/{id}", a.deleteToken)
	private("GET /api/ssh-keys", a.listSSHKeys)
	private("POST /api/ssh-keys", a.addSSHKey)
	private("DELETE /api/ssh-keys/{id}", a.deleteSSHKey)
	private("POST /api/export", a.export)
	private("POST /api/import", a.importSettings)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "неизвестный адрес API")
	})
	return mux
}

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errorf(status int, format string, args ...any) error {
	return &httpError{status: status, msg: fmt.Sprintf(format, args...)}
}

func (a *API) wrap(h func(http.ResponseWriter, *http.Request) error, needSession bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Together with SameSite=Strict cookies this blocks cross-site
			// form posts: browsers cannot send JSON across sites without CORS.
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "API принимает изменения только с Content-Type: application/json")
				return
			}
		}
		if needSession && !a.Auth.ValidSession(r.Context(), auth.SessionFromRequest(r)) {
			writeError(w, http.StatusUnauthorized, "нужно войти")
			return
		}
		if err := h(w, r); err != nil {
			a.handleError(w, r, err)
		}
	})
}

func (a *API) handleError(w http.ResponseWriter, r *http.Request, err error) {
	if he, ok := errors.AsType[*httpError](err); ok {
		writeError(w, he.status, he.msg)
		return
	}
	if ve, ok := errors.AsType[*core.ValidationError](err); ok {
		writeError(w, http.StatusBadRequest, ve.Msg)
		return
	}
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "не найдено")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "уже существует")
	default:
		a.Log.Error("API request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка, подробности в журнале сервера")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errorf(http.StatusBadRequest, "некорректный JSON: %v", err)
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errorf(http.StatusNotFound, "не найдено")
	}
	return id, nil
}

// Session.

func (a *API) session(w http.ResponseWriter, r *http.Request) error {
	name, exists, err := a.Auth.Owner(r.Context())
	if err != nil {
		return err
	}
	authenticated := a.Auth.ValidSession(r.Context(), auth.SessionFromRequest(r))
	resp := map[string]any{"owner_exists": exists, "authenticated": authenticated}
	if authenticated {
		resp["owner_name"] = name
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if _, exists, err := a.Auth.Owner(r.Context()); err != nil {
		return err
	} else if !exists {
		return errorf(http.StatusConflict, "владелец не создан: выполните на сервере metagit owner create")
	}
	id, err := a.Auth.Login(r.Context(), in.Name, in.Password, r.RemoteAddr)
	switch {
	case errors.Is(err, auth.ErrTooManyAttempts):
		return errorf(http.StatusTooManyRequests, "слишком много неудачных попыток входа, попробуйте позже")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return errorf(http.StatusUnauthorized, "неверное имя или пароль")
	case err != nil:
		return err
	}
	auth.SetSessionCookie(w, id, a.Auth.SessionTTL(), a.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "owner_name": in.Name})
	return nil
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) error {
	if err := a.Auth.Logout(r.Context(), auth.SessionFromRequest(r)); err != nil {
		return err
	}
	auth.ClearSessionCookie(w, a.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
	return nil
}

// Projects.

type projectJSON struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	HTTPURL   string    `json:"http_url"`
	SSHURL    string    `json:"ssh_url"`
}

func (a *API) projectJSON(p store.Project) projectJSON {
	return projectJSON{
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		HTTPURL:   strings.TrimSuffix(a.BaseURL, "/") + "/" + p.Name + ".git",
		SSHURL:    strings.TrimSuffix(a.SSHURL, "/") + "/" + p.Name + ".git",
	}
}

type projectSummaryJSON struct {
	projectJSON
	Targets []targetJSON `json:"targets"`
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) error {
	projects, err := a.Core.Store.ListProjects(r.Context())
	if err != nil {
		return err
	}
	out := make([]projectSummaryJSON, 0, len(projects))
	for _, p := range projects {
		targets, err := a.Core.Store.ListTargets(r.Context(), p.ID)
		if err != nil {
			return err
		}
		out = append(out, projectSummaryJSON{projectJSON: a.projectJSON(p), Targets: targetsJSON(targets)})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	p, err := a.Core.CreateProject(r.Context(), strings.TrimSpace(in.Name))
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "проект %q уже существует", in.Name)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, a.projectJSON(*p))
	return nil
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) error {
	p, err := a.Core.Project(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	targets, err := a.Core.Store.ListTargets(r.Context(), p.ID)
	if err != nil {
		return err
	}
	protected, err := a.Core.Store.ListProtected(r.Context(), p.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":   a.projectJSON(*p),
		"targets":   targetsJSON(targets),
		"protected": protectedJSON(protected),
	})
	return nil
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.Core.DeleteProject(r.Context(), r.PathValue("name"), in.Confirm); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	return nil
}

// Targets.

type targetJSON struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	AuthKind      string    `json:"auth_kind"`
	Username      string    `json:"username"`
	HasSecret     bool      `json:"has_secret"`
	Enabled       bool      `json:"enabled"`
	State         string    `json:"state"`
	LastError     string    `json:"last_error"`
	LastSuccessAt time.Time `json:"last_success_at,omitzero"`
	NextAttemptAt time.Time `json:"next_attempt_at,omitzero"`
}

func targetsJSON(targets []store.Target) []targetJSON {
	out := make([]targetJSON, 0, len(targets))
	for _, t := range targets {
		out = append(out, targetJSON{
			ID: t.ID, Name: t.Name, URL: t.URL, AuthKind: t.AuthKind, Username: t.Username,
			HasSecret: len(t.Secret) > 0, Enabled: t.Enabled, State: t.State, LastError: t.LastError,
			LastSuccessAt: t.LastSuccessAt, NextAttemptAt: t.NextAttemptAt,
		})
	}
	return out
}

type targetInput struct {
	Name     string  `json:"name"`
	URL      string  `json:"url"`
	AuthKind string  `json:"auth_kind"`
	Username string  `json:"username"`
	Secret   *string `json:"secret"`
	Enabled  *bool   `json:"enabled"`
}

func (in targetInput) core() core.TargetInput {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	secret := in.Secret
	if secret != nil && *secret == "" {
		// An empty field in the form means "keep the current secret".
		secret = nil
	}
	return core.TargetInput{Name: in.Name, URL: in.URL, AuthKind: in.AuthKind, Username: in.Username, Secret: secret, Enabled: enabled}
}

func (a *API) listTargets(w http.ResponseWriter, r *http.Request) error {
	targets, err := a.Core.Targets(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, targetsJSON(targets))
	return nil
}

func (a *API) createTarget(w http.ResponseWriter, r *http.Request) error {
	var in targetInput
	if err := decode(r, &in); err != nil {
		return err
	}
	t, err := a.Core.AddTarget(r.Context(), r.PathValue("name"), in.core())
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "target %q уже есть в проекте", in.Name)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, targetsJSON([]store.Target{*t})[0])
	return nil
}

func (a *API) updateTarget(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in targetInput
	if err := decode(r, &in); err != nil {
		return err
	}
	t, err := a.Core.UpdateTarget(r.Context(), r.PathValue("name"), id, in.core())
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "target %q уже есть в проекте", in.Name)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, targetsJSON([]store.Target{*t})[0])
	return nil
}

func (a *API) deleteTarget(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := a.Core.DeleteTarget(r.Context(), r.PathValue("name"), id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	return nil
}

func (a *API) syncTarget(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := a.Core.SyncTarget(r.Context(), r.PathValue("name"), id); err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
	return nil
}

// Protected refs.

type protectedRefJSON struct {
	ID      int64  `json:"id"`
	Pattern string `json:"pattern"`
}

func protectedJSON(list []store.ProtectedRef) []protectedRefJSON {
	out := make([]protectedRefJSON, 0, len(list))
	for _, p := range list {
		out = append(out, protectedRefJSON{ID: p.ID, Pattern: p.Pattern})
	}
	return out
}

func (a *API) listProtected(w http.ResponseWriter, r *http.Request) error {
	list, err := a.Core.Protected(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, protectedJSON(list))
	return nil
}

func (a *API) addProtected(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	p, err := a.Core.AddProtected(r.Context(), r.PathValue("name"), in.Pattern)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "шаблон %q уже есть", in.Pattern)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, protectedRefJSON{ID: p.ID, Pattern: p.Pattern})
	return nil
}

func (a *API) deleteProtected(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := a.Core.DeleteProtected(r.Context(), r.PathValue("name"), id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	return nil
}

// Events and archive.

type eventJSON struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	TargetName string    `json:"target_name"`
	Ref        string    `json:"ref"`
	ArchiveRef string    `json:"archive_ref"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request) error {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			return errorf(http.StatusBadRequest, "limit должен быть от 1 до 1000")
		}
		limit = n
	}
	events, err := a.Core.Events(r.Context(), r.PathValue("name"), limit)
	if err != nil {
		return err
	}
	out := make([]eventJSON, 0, len(events))
	for _, e := range events {
		out = append(out, eventJSON{ID: e.ID, Kind: e.Kind, TargetName: e.TargetName, Ref: e.Ref,
			ArchiveRef: e.ArchiveRef, Message: e.Message, CreatedAt: e.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

type archiveJSON struct {
	ID           int64     `json:"id"`
	Source       string    `json:"source"`
	TargetName   string    `json:"target_name"`
	OrigRef      string    `json:"orig_ref"`
	ArchiveRef   string    `json:"archive_ref"`
	Object       string    `json:"object"`
	CreatedAt    time.Time `json:"created_at"`
	FetchCommand string    `json:"fetch_command"`
}

func (a *API) listArchive(w http.ResponseWriter, r *http.Request) error {
	p, err := a.Core.Project(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	list, err := a.Core.Store.ListArchiveRefs(r.Context(), p.ID)
	if err != nil {
		return err
	}
	url := a.projectJSON(*p).HTTPURL
	out := make([]archiveJSON, 0, len(list))
	for _, ar := range list {
		out = append(out, archiveJSON{ID: ar.ID, Source: ar.Source, TargetName: ar.TargetName, OrigRef: ar.OrigRef,
			ArchiveRef: ar.ArchiveRef, Object: ar.Object, CreatedAt: ar.CreatedAt,
			FetchCommand: "git fetch " + url + " " + ar.ArchiveRef})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// Tokens and SSH keys.

type tokenJSON struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) error {
	tokens, err := a.Auth.ListTokens(r.Context())
	if err != nil {
		return err
	}
	out := make([]tokenJSON, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenJSON{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	value, t, err := a.Auth.CreateToken(r.Context(), in.Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": tokenJSON{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt},
		"value": value,
	})
	return nil
}

func (a *API) deleteToken(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := a.Auth.RevokeToken(r.Context(), id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	return nil
}

type sshKeyJSON struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a *API) listSSHKeys(w http.ResponseWriter, r *http.Request) error {
	keys, err := a.Auth.ListSSHKeys(r.Context())
	if err != nil {
		return err
	}
	out := make([]sshKeyJSON, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshKeyJSON{ID: k.ID, Name: k.Name, Fingerprint: k.Fingerprint, CreatedAt: k.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) addSSHKey(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	k, err := a.Auth.AddSSHKey(r.Context(), in.Name, in.PublicKey)
	if errors.Is(err, store.ErrConflict) {
		return errorf(http.StatusConflict, "этот ключ уже добавлен")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, sshKeyJSON{ID: k.ID, Name: k.Name, Fingerprint: k.Fingerprint, CreatedAt: k.CreatedAt})
	return nil
}

func (a *API) deleteSSHKey(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := a.Auth.DeleteSSHKey(r.Context(), id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	return nil
}

// Export and import.

func (a *API) export(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	data, err := a.Transfer.Export(r.Context(), in.Passphrase)
	if err != nil {
		return err
	}
	name := "metagit-settings-" + time.Now().UTC().Format("20060102-150405") + ".age"
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return nil
}

func (a *API) importSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Passphrase string `json:"passphrase"`
		// Data is the export file in base64.
		Data string `json:"data"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		return errorf(http.StatusBadRequest, "файл должен быть передан в base64")
	}
	created, err := a.Transfer.Import(r.Context(), data, in.Passphrase)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": created})
	return nil
}

// Package core implements metagit operations on projects, targets and
// protected refs, and decides on pushes for git hooks.
package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/archive"
	"github.com/ekalinin/metagit/internal/hooks"
	"github.com/ekalinin/metagit/internal/replicate"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/secrets"
	"github.com/ekalinin/metagit/internal/store"
)

// Errors of operations. ValidationError carries a message for the user.
var (
	ErrNotFound = store.ErrNotFound
	ErrConflict = store.ErrConflict
)

// ValidationError is an error caused by user input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Service implements the operations.
type Service struct {
	Store      *store.Store
	Repos      *repo.Manager
	Box        *secrets.Box
	Archive    *archive.Service
	Replicator *replicate.Replicator
	Log        *slog.Logger
	// PushStatusTimeout limits how long a push waits for delivery results.
	PushStatusTimeout time.Duration
}

// Project returns a project by name.
func (s *Service) Project(ctx context.Context, name string) (*store.Project, error) {
	return s.Store.ProjectByName(ctx, name)
}

// ProjectExists reports whether a project and its repository exist.
func (s *Service) ProjectExists(ctx context.Context, name string) bool {
	if _, err := s.Store.ProjectByName(ctx, name); err != nil {
		return false
	}
	return s.Repos.Exists(name)
}

// CreateProject creates a project with an empty repository.
func (s *Service) CreateProject(ctx context.Context, name string) (*store.Project, error) {
	if err := repo.ValidateName(name); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}
	p, err := s.Store.CreateProject(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := s.Repos.Create(ctx, name); err != nil {
		_ = s.Store.DeleteProject(context.WithoutCancel(ctx), p.ID)
		if errors.Is(err, repo.ErrExists) {
			return nil, fmt.Errorf("repository directory %s already exists on disk: %w", s.Repos.Path(name), ErrConflict)
		}
		return nil, err
	}
	return &p, nil
}

// DeleteProject removes a project, its repository and archive. confirm must
// repeat the name. Repositories on targets are not touched.
func (s *Service) DeleteProject(ctx context.Context, name, confirm string) error {
	p, err := s.Store.ProjectByName(ctx, name)
	if err != nil {
		return err
	}
	if confirm != name {
		return invalid("to delete, type the project name %q", name)
	}
	targets, err := s.Store.ListTargets(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, t := range targets {
		s.Replicator.RemoveTarget(t.ID)
	}
	if err := s.Store.DeleteProject(ctx, p.ID); err != nil {
		return err
	}
	return s.Repos.Delete(name)
}

// TargetInput are target settings from the user.
type TargetInput struct {
	Name     string
	URL      string
	AuthKind string
	Username string
	// Secret is the token or SSH private key. nil keeps the current one.
	Secret  *string
	Enabled bool
}

// NormalizeTarget trims and validates target settings. haveSecret tells
// whether a stored secret can be kept when in.Secret is nil.
func NormalizeTarget(in *TargetInput, haveSecret bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	in.Username = strings.TrimSpace(in.Username)
	in.AuthKind = cmp.Or(in.AuthKind, store.AuthNone)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return invalid("target name must be non-empty and at most 100 characters")
	}
	if err := replicate.ValidateURL(in.URL); err != nil {
		return &ValidationError{Msg: err.Error()}
	}
	if !slices.Contains([]string{store.AuthNone, store.AuthHTTPS, store.AuthSSH}, in.AuthKind) {
		return invalid("credentials kind must be none, https or ssh")
	}
	if in.AuthKind == store.AuthNone {
		return nil
	}
	if in.Secret == nil {
		if !haveSecret {
			return invalid("credentials are required: a token for https or a private SSH key for ssh")
		}
		return nil
	}
	secret := strings.TrimSpace(*in.Secret)
	if secret == "" {
		return invalid("credentials must not be empty")
	}
	if in.AuthKind == store.AuthSSH {
		if _, err := ssh.ParseRawPrivateKey([]byte(secret)); err != nil {
			if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); ok {
				return invalid("SSH key is protected by a passphrase: a key without a passphrase is required")
			}
			return invalid("cannot parse the private SSH key: %v", err)
		}
		secret += "\n"
	}
	in.Secret = &secret
	return nil
}

// Targets returns the targets of a project.
func (s *Service) Targets(ctx context.Context, projectName string) ([]store.Target, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	return s.Store.ListTargets(ctx, p.ID)
}

// AddTarget adds a target and starts the first delivery.
func (s *Service) AddTarget(ctx context.Context, projectName string, in TargetInput) (*store.Target, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	if err := NormalizeTarget(&in, false); err != nil {
		return nil, err
	}
	t := &store.Target{ProjectID: p.ID, Name: in.Name, URL: in.URL, AuthKind: in.AuthKind, Username: in.Username, Enabled: in.Enabled}
	if in.AuthKind != store.AuthNone {
		if t.Secret, err = s.Box.Seal([]byte(*in.Secret)); err != nil {
			return nil, err
		}
	}
	if err := s.Store.CreateTarget(ctx, t); err != nil {
		return nil, err
	}
	if t.Enabled {
		s.Replicator.Trigger(ctx, t.ID)
	}
	return t, nil
}

func (s *Service) projectTarget(ctx context.Context, projectName string, id int64) (*store.Project, *store.Target, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, nil, err
	}
	t, err := s.Store.TargetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if t.ProjectID != p.ID {
		return nil, nil, ErrNotFound
	}
	return p, t, nil
}

// UpdateTarget changes target settings. An enabled target is delivered to
// right away.
func (s *Service) UpdateTarget(ctx context.Context, projectName string, id int64, in TargetInput) (*store.Target, error) {
	_, t, err := s.projectTarget(ctx, projectName, id)
	if err != nil {
		return nil, err
	}
	sameKind := in.AuthKind == t.AuthKind || (in.AuthKind == "" && t.AuthKind == store.AuthNone)
	if err := NormalizeTarget(&in, sameKind && len(t.Secret) > 0); err != nil {
		return nil, err
	}
	t.Name, t.URL, t.AuthKind, t.Username, t.Enabled = in.Name, in.URL, in.AuthKind, in.Username, in.Enabled
	switch {
	case in.AuthKind == store.AuthNone:
		t.Secret = nil
	case in.Secret != nil:
		if t.Secret, err = s.Box.Seal([]byte(*in.Secret)); err != nil {
			return nil, err
		}
	}
	if err := s.Store.UpdateTarget(ctx, t); err != nil {
		return nil, err
	}
	if t.Enabled {
		s.Replicator.Trigger(ctx, t.ID)
	}
	return s.Store.TargetByID(ctx, t.ID)
}

// DeleteTarget removes a target. The repository on the hosting stays.
func (s *Service) DeleteTarget(ctx context.Context, projectName string, id int64) error {
	p, t, err := s.projectTarget(ctx, projectName, id)
	if err != nil {
		return err
	}
	s.Replicator.RemoveTarget(t.ID)
	return s.Store.DeleteTarget(ctx, p.ID, t.ID)
}

// SyncTarget starts a delivery without waiting for the retry timer.
func (s *Service) SyncTarget(ctx context.Context, projectName string, id int64) error {
	_, t, err := s.projectTarget(ctx, projectName, id)
	if err != nil {
		return err
	}
	if !t.Enabled {
		return invalid("target %s is disabled", t.Name)
	}
	s.Replicator.Trigger(ctx, t.ID)
	return nil
}

// Protected returns protected ref patterns of a project.
func (s *Service) Protected(ctx context.Context, projectName string) ([]store.ProtectedRef, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	return s.Store.ListProtected(ctx, p.ID)
}

// AddProtected adds a protected ref pattern.
func (s *Service) AddProtected(ctx context.Context, projectName, pattern string) (*store.ProtectedRef, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	pattern = strings.TrimSpace(pattern)
	if err := archive.ValidatePattern(pattern); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}
	pr, err := s.Store.AddProtected(ctx, p.ID, pattern)
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

// DeleteProtected removes a protected ref pattern.
func (s *Service) DeleteProtected(ctx context.Context, projectName string, id int64) error {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return err
	}
	return s.Store.DeleteProtected(ctx, p.ID, id)
}

// Events returns the newest events of a project.
func (s *Service) Events(ctx context.Context, projectName string, limit int) ([]store.Event, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	return s.Store.ListEvents(ctx, p.ID, limit)
}

// ArchiveRefs returns archive records of a project.
func (s *Service) ArchiveRefs(ctx context.Context, projectName string) ([]store.ArchiveRef, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	return s.Store.ListArchiveRefs(ctx, p.ID)
}

// PreReceive implements hooks.Handler.
func (s *Service) PreReceive(ctx context.Context, projectName, pushID string, updates []hooks.Update) (hooks.Response, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return hooks.Response{}, err
	}
	return s.Archive.PreReceive(ctx, p, pushID, updates)
}

// PostReceive implements hooks.Handler: it archives previous values, starts
// delivery and reports results that arrive within PushStatusTimeout.
func (s *Service) PostReceive(ctx context.Context, projectName, pushID string, _ []hooks.Update) (hooks.Response, error) {
	p, err := s.Store.ProjectByName(ctx, projectName)
	if err != nil {
		return hooks.Response{}, err
	}
	var msgs []string
	archived, err := s.Archive.Complete(ctx, p, pushID)
	for _, a := range archived {
		msgs = append(msgs, fmt.Sprintf("metagit: %s: previous state saved in %s", a.OrigRef, a.ArchiveRef))
	}
	if err != nil {
		s.Log.Error("archive failed", "project", p.Name, "err", err)
		msgs = append(msgs, "metagit: cannot create the archive, it will be created on the next start: "+err.Error())
	}

	tickets, err := s.Replicator.TriggerProject(ctx, p.ID)
	if err != nil {
		return hooks.Response{}, err
	}
	if len(tickets) == 0 {
		msgs = append(msgs, "metagit: no enabled targets, the changes are saved only in metagit")
		return hooks.Response{Allow: true, Messages: msgs}, nil
	}
	targets, err := s.Store.ListTargets(ctx, p.ID)
	if err != nil {
		return hooks.Response{}, err
	}
	wctx, cancel := context.WithTimeout(ctx, s.PushStatusTimeout)
	defer cancel()
	for _, t := range targets {
		ticket, ok := tickets[t.ID]
		if !ok {
			continue
		}
		res, done := s.Replicator.Wait(wctx, t.ID, ticket)
		msgs = append(msgs, FormatResult(t.Name, res, done))
	}
	return hooks.Response{Allow: true, Messages: msgs}, nil
}

// FormatResult is the push output line for a target.
func FormatResult(name string, res replicate.Result, done bool) string {
	prefix := "metagit: " + name + ": "
	if !done {
		return prefix + "queued"
	}
	switch res.Status {
	case replicate.StatusDelivered:
		return prefix + "delivered"
	case replicate.StatusDiverged:
		return prefix + "delivered; diverged, hosting state saved in " + strings.Join(res.Archives, ", ")
	case replicate.StatusError:
		msg, _, _ := strings.Cut(res.Error, "\n")
		if utf8.RuneCountInString(msg) > 200 {
			msg = string([]rune(msg)[:200]) + "..."
		}
		return prefix + "error: " + msg + " (retry later)"
	default:
		return prefix + string(res.Status)
	}
}

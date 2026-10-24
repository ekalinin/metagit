// Package archive protects refs from destructive pushes and keeps previous
// states of refs under refs/metagit/archive/.
package archive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/hooks"
	"github.com/ekalinin/metagit/internal/notify"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/store"
)

// Namespaces of metagit refs.
const (
	Namespace = "refs/metagit/"
	Prefix    = "refs/metagit/archive/"
	tmpPrefix = "refs/metagit/tmp/"
)

// TimeFormat is the time part of archive ref names.
const TimeFormat = "20060102T150405Z"

// Name returns the base archive ref name for ref. targetID is 0 for the push
// source.
func Name(source string, targetID int64, ref string, t time.Time) string {
	rel := strings.TrimPrefix(ref, "refs/")
	ts := t.UTC().Format(TimeFormat)
	if source == store.SourceTarget {
		return Prefix + "targets/" + strconv.FormatInt(targetID, 10) + "/" + rel + "/" + ts
	}
	return Prefix + "push/" + rel + "/" + ts
}

// TmpRef is where a divergent ref of a target is fetched before archiving.
func TmpRef(targetID int64) string {
	return tmpPrefix + strconv.FormatInt(targetID, 10)
}

// MatchPattern reports whether ref matches a protection pattern. '*' matches
// any sequence of characters, including '/'.
func MatchPattern(pattern, ref string) bool {
	re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	ok, err := regexp.MatchString(re, ref)
	return err == nil && ok
}

// ValidatePattern checks a protection pattern.
func ValidatePattern(pattern string) error {
	if !strings.HasPrefix(pattern, "refs/") || len(pattern) <= len("refs/") || strings.ContainsAny(pattern, " \t\n") {
		return errors.New("pattern must start with refs/, for example refs/heads/main or refs/tags/*")
	}
	if strings.HasPrefix(pattern, Namespace) {
		return errors.New("refs/metagit/ is a reserved namespace, it does not need protection")
	}
	return nil
}

// Check returns rejection messages for a push, or nil when it is allowed.
func Check(updates []hooks.Update, patterns []string) []string {
	var msgs []string
	for _, u := range updates {
		if strings.HasPrefix(u.Ref, Namespace) {
			msgs = append(msgs, fmt.Sprintf("metagit: %s: refs/metagit/ is a reserved namespace, pushes to it are not allowed", u.Ref))
			continue
		}
		if !u.Kind.Destructive() {
			continue
		}
		for _, p := range patterns {
			if MatchPattern(p, u.Ref) {
				msgs = append(msgs, fmt.Sprintf("metagit: %s is protected (pattern %s): %s is not allowed", u.Ref, p, describe(u.Kind)))
				break
			}
		}
	}
	if len(msgs) > 0 {
		msgs = append(msgs, "metagit: push rejected, no refs were changed")
	}
	return msgs
}

func describe(k hooks.Kind) string {
	switch k {
	case hooks.KindDelete:
		return "deletion"
	case hooks.KindTagMove:
		return "tag move"
	default:
		return "non-fast-forward push"
	}
}

// Service creates archive refs and records them.
type Service struct {
	Store *store.Store
	Repos *repo.Manager
	Git   *gitcmd.Runner
	Log   *slog.Logger
	Now   func() time.Time
	// Notifier gets archive_push events; nil when notifications are off.
	Notifier *notify.Notifier
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// PreReceive checks a push and remembers previous values of refs that the
// push is going to rewrite or delete.
func (s *Service) PreReceive(ctx context.Context, project *store.Project, pushID string, updates []hooks.Update) (hooks.Response, error) {
	protected, err := s.Store.ListProtected(ctx, project.ID)
	if err != nil {
		return hooks.Response{}, err
	}
	patterns := make([]string, len(protected))
	for i, p := range protected {
		patterns[i] = p.Pattern
	}
	if msgs := Check(updates, patterns); len(msgs) > 0 {
		return hooks.Response{Allow: false, Messages: msgs}, nil
	}
	err = s.Store.WithTx(ctx, func(tx *store.Store) error {
		for _, u := range updates {
			if !u.Kind.Destructive() {
				continue
			}
			in := &store.ArchiveIntent{ProjectID: project.ID, PushID: pushID, Ref: u.Ref, OldObject: u.Old, NewObject: u.New}
			if err := tx.AddArchiveIntent(ctx, in); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return hooks.Response{}, err
	}
	return hooks.Response{Allow: true}, nil
}

// Complete archives previous values for the intents of a finished push.
func (s *Service) Complete(ctx context.Context, project *store.Project, pushID string) ([]store.ArchiveRef, error) {
	intents, err := s.Store.PendingIntents(ctx, pushID)
	if err != nil {
		return nil, err
	}
	return s.finish(ctx, project, intents)
}

// Recover archives intents left by a crash. It is called on start.
func (s *Service) Recover(ctx context.Context) error {
	intents, err := s.Store.AllPendingIntents(ctx)
	if err != nil {
		return err
	}
	byProject := map[int64][]store.ArchiveIntent{}
	for _, in := range intents {
		byProject[in.ProjectID] = append(byProject[in.ProjectID], in)
	}
	var errs []error
	for projectID, list := range byProject {
		project, err := s.Store.ProjectByID(ctx, projectID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		archived, err := s.finish(ctx, project, list)
		if err != nil {
			errs = append(errs, err)
		}
		for _, a := range archived {
			s.Log.Info("archive created on recovery", "project", project.Name, "ref", a.OrigRef, "archive", a.ArchiveRef)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) finish(ctx context.Context, project *store.Project, intents []store.ArchiveIntent) ([]store.ArchiveRef, error) {
	gitDir := s.Repos.Path(project.Name)
	var archived []store.ArchiveRef
	for _, in := range intents {
		current, err := s.Git.ResolveRef(ctx, gitDir, in.Ref)
		if err != nil {
			return archived, err
		}
		if current == in.OldObject {
			// The push did not change the ref (rejected or failed).
			if err := s.Store.FinishIntent(ctx, in.ID); err != nil {
				return archived, err
			}
			continue
		}
		name, err := s.createRef(ctx, gitDir, Name(store.SourcePush, 0, in.Ref, in.CreatedAt), in.OldObject)
		if err != nil {
			return archived, fmt.Errorf("archive %s: %w", in.Ref, err)
		}
		a := store.ArchiveRef{ProjectID: project.ID, Source: store.SourcePush, OrigRef: in.Ref, ArchiveRef: name, Object: in.OldObject, CreatedAt: in.CreatedAt}
		msg := fmt.Sprintf("%s: previous state %s saved in %s", in.Ref, short(in.OldObject), name)
		err = s.Store.WithTx(ctx, func(tx *store.Store) error {
			if err := tx.AddArchiveRef(ctx, &a); err != nil {
				return err
			}
			err := tx.AddEvent(ctx, &store.Event{
				ProjectID:  project.ID,
				Kind:       store.EventArchivePush,
				Ref:        in.Ref,
				ArchiveRef: name,
				Message:    msg,
			})
			if err != nil {
				return err
			}
			return tx.FinishIntent(ctx, in.ID)
		})
		if err != nil {
			return archived, err
		}
		s.Notifier.Notify(notify.Event{Kind: notify.KindArchivePush, Project: project.Name, Ref: in.Ref, ArchiveRef: name, Message: msg})
		archived = append(archived, a)
	}
	return archived, nil
}

// SaveTargetState archives the object a target had for ref before metagit
// overwrites it. The object must already be in the repository.
func (s *Service) SaveTargetState(ctx context.Context, project *store.Project, target *store.Target, ref, object string) (string, error) {
	now := s.now()
	name, err := s.createRef(ctx, s.Repos.Path(project.Name), Name(store.SourceTarget, target.ID, ref, now), object)
	if err != nil {
		return "", err
	}
	a := store.ArchiveRef{ProjectID: project.ID, Source: store.SourceTarget, TargetID: target.ID, TargetName: target.Name,
		OrigRef: ref, ArchiveRef: name, Object: object, CreatedAt: now}
	if err := s.Store.AddArchiveRef(ctx, &a); err != nil {
		return "", err
	}
	return name, nil
}

// createRef creates base (or base-2, base-3, ...) pointing to object. When a
// candidate already points to object it is reused, so retries are idempotent.
func (s *Service) createRef(ctx context.Context, gitDir, base, object string) (string, error) {
	for i := 1; i <= 100; i++ {
		name := base
		if i > 1 {
			name = base + "-" + strconv.Itoa(i)
		}
		existing, err := s.Git.ResolveRef(ctx, gitDir, name)
		if err != nil {
			return "", err
		}
		if existing == object {
			return name, nil
		}
		if existing != "" {
			continue
		}
		// An empty old value means "the ref must not exist".
		if _, err := s.Git.Run(ctx, gitcmd.Opts{Dir: gitDir}, "update-ref", "-m", "metagit archive", name, object, ""); err != nil {
			if existing, _ := s.Git.ResolveRef(ctx, gitDir, name); existing != "" {
				continue
			}
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("no free archive name for %s", base)
}

func short(oid string) string {
	if len(oid) > 12 {
		return oid[:12]
	}
	return oid
}

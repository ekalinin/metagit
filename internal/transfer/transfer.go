// Package transfer exports metagit settings to a passphrase-encrypted file
// and imports them on another server.
package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/ekalinin/metagit/internal/archive"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/secrets"
	"github.com/ekalinin/metagit/internal/store"
)

// Version is the format version of the export file.
const Version = 1

// MinPassphraseLength is the minimal length of the export passphrase.
const MinPassphraseLength = 8

// Settings is the content of an export file.
type Settings struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	Projects   []Project `json:"projects"`
}

// Project is an exported project.
type Project struct {
	Name          string   `json:"name"`
	ProtectedRefs []string `json:"protected_refs"`
	Targets       []Target `json:"targets"`
}

// Target is an exported target with its credentials.
type Target struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	AuthKind string `json:"auth_kind"`
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// Service exports and imports settings.
type Service struct {
	Store *store.Store
	Repos *repo.Manager
	Box   *secrets.Box
	// WorkFactor is the scrypt work factor (log2 N); 0 uses the age default.
	WorkFactor int
}

func invalid(format string, args ...any) error {
	return &core.ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Export returns the settings encrypted with the passphrase (age, armored).
func (s *Service) Export(ctx context.Context, passphrase string) ([]byte, error) {
	if utf8.RuneCountInString(passphrase) < MinPassphraseLength {
		return nil, invalid("export file password must be at least %d characters", MinPassphraseLength)
	}
	settings := Settings{Version: Version, ExportedAt: time.Now().UTC(), Projects: []Project{}}
	projects, err := s.Store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		ep := Project{Name: p.Name, ProtectedRefs: []string{}, Targets: []Target{}}
		protected, err := s.Store.ListProtected(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, pr := range protected {
			ep.ProtectedRefs = append(ep.ProtectedRefs, pr.Pattern)
		}
		targets, err := s.Store.ListTargets(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			secret, err := s.Box.Open(t.Secret)
			if err != nil {
				return nil, fmt.Errorf("target %s/%s: %w", p.Name, t.Name, err)
			}
			ep.Targets = append(ep.Targets, Target{Name: t.Name, URL: t.URL, AuthKind: t.AuthKind,
				Username: t.Username, Secret: string(secret), Enabled: t.Enabled})
		}
		settings.Projects = append(settings.Projects, ep)
	}
	plain, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	if s.WorkFactor > 0 {
		recipient.SetWorkFactor(s.WorkFactor)
	}
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := aw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decrypt opens an export file.
func Decrypt(data []byte, passphrase string) (*Settings, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, invalid("invalid password")
	}
	var src io.Reader = bytes.NewReader(data)
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(armor.Header)) {
		src = armor.NewReader(bytes.NewReader(bytes.TrimSpace(data)))
	}
	r, err := age.Decrypt(src, identity)
	if err != nil {
		return nil, invalid("cannot decrypt the file: invalid password or corrupted file")
	}
	plain, err := io.ReadAll(io.LimitReader(r, 64<<20))
	if err != nil {
		return nil, invalid("cannot decrypt the file: corrupted file")
	}
	var settings Settings
	dec := json.NewDecoder(bytes.NewReader(plain))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		return nil, invalid("export file is corrupted: %v", err)
	}
	if settings.Version != Version {
		return nil, invalid("unsupported export file version: %d", settings.Version)
	}
	return &settings, nil
}

// Import creates projects (with empty repositories), their protected refs
// and targets. It changes nothing when any part fails. It returns the names
// of created projects.
func (s *Service) Import(ctx context.Context, data []byte, passphrase string) ([]string, error) {
	settings, err := Decrypt(data, passphrase)
	if err != nil {
		return nil, err
	}
	if err := s.validate(ctx, settings); err != nil {
		return nil, err
	}

	var created []string
	err = s.Store.WithTx(ctx, func(tx *store.Store) error {
		for _, ep := range settings.Projects {
			p, err := tx.CreateProject(ctx, ep.Name)
			if err != nil {
				return fmt.Errorf("project %s: %w", ep.Name, err)
			}
			for _, pattern := range ep.ProtectedRefs {
				if _, err := tx.AddProtected(ctx, p.ID, pattern); err != nil && !errors.Is(err, store.ErrConflict) {
					return err
				}
			}
			for _, et := range ep.Targets {
				t := &store.Target{ProjectID: p.ID, Name: et.Name, URL: et.URL, AuthKind: et.AuthKind, Username: et.Username, Enabled: et.Enabled}
				if et.AuthKind != store.AuthNone {
					if t.Secret, err = s.Box.Seal([]byte(et.Secret)); err != nil {
						return err
					}
				}
				if err := tx.CreateTarget(ctx, t); err != nil {
					return fmt.Errorf("target %s/%s: %w", ep.Name, et.Name, err)
				}
			}
		}
		for _, ep := range settings.Projects {
			if err := s.Repos.Create(ctx, ep.Name); err != nil {
				return fmt.Errorf("repository %s: %w", ep.Name, err)
			}
			created = append(created, ep.Name)
		}
		return nil
	})
	if err != nil {
		for _, name := range created {
			_ = s.Repos.Delete(name)
		}
		return nil, err
	}
	return created, nil
}

func (s *Service) validate(ctx context.Context, settings *Settings) error {
	seen := map[string]bool{}
	var conflicts []string
	for i := range settings.Projects {
		ep := &settings.Projects[i]
		if err := repo.ValidateName(ep.Name); err != nil {
			return invalid("project %q: %v", ep.Name, err)
		}
		if seen[ep.Name] {
			return invalid("project %q appears in the file twice", ep.Name)
		}
		seen[ep.Name] = true
		if _, err := s.Store.ProjectByName(ctx, ep.Name); err == nil || s.Repos.Exists(ep.Name) {
			conflicts = append(conflicts, ep.Name)
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		for _, pattern := range ep.ProtectedRefs {
			if err := archive.ValidatePattern(pattern); err != nil {
				return invalid("project %q, pattern %q: %v", ep.Name, pattern, err)
			}
		}
		targetNames := map[string]bool{}
		for j := range ep.Targets {
			et := &ep.Targets[j]
			in := core.TargetInput{Name: et.Name, URL: et.URL, AuthKind: et.AuthKind, Username: et.Username, Enabled: et.Enabled}
			if et.AuthKind != store.AuthNone && et.AuthKind != "" {
				in.Secret = &et.Secret
			}
			if err := core.NormalizeTarget(&in, false); err != nil {
				return invalid("project %q, target %q: %v", ep.Name, et.Name, err)
			}
			if targetNames[in.Name] {
				return invalid("project %q: target %q appears twice", ep.Name, in.Name)
			}
			targetNames[in.Name] = true
			et.Name, et.URL, et.AuthKind, et.Username = in.Name, in.URL, in.AuthKind, in.Username
			if in.Secret != nil {
				et.Secret = *in.Secret
			}
		}
	}
	if len(conflicts) > 0 {
		slices.Sort(conflicts)
		return &core.ValidationError{Msg: "import rejected: projects already exist: " + strings.Join(conflicts, ", ")}
	}
	return nil
}

package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "metagit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsCreateTables(t *testing.T) {
	s := open(t)
	var names []string
	rows, err := s.db.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, want := range []string{"owner", "sessions", "access_tokens", "ssh_keys", "projects", "protected_refs",
		"targets", "target_refs", "archive_intents", "archive_refs", "events", "schema_migrations"} {
		if !slices.Contains(names, want) {
			t.Errorf("table %s is missing (have %v)", want, names)
		}
	}
}

func TestDatabaseFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metagit.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(t.Context(), "foo"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, name := range []string{path, path + "-wal"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v", filepath.Base(name), info.Mode().Perm())
		}
	}
}

func TestReopenDoesNotReapplyMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metagit.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(t.Context(), "foo"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	var n int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations has %d rows", n)
	}
	if _, err := s.ProjectByName(t.Context(), "foo"); err != nil {
		t.Fatalf("data lost after reopen: %v", err)
	}
}

func TestOwner(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	if _, err := s.GetOwner(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.CreateOwner(ctx, "eugene", "h1"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOwner(ctx, "other", "h2"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second owner: %v", err)
	}
	if err := s.UpdateOwnerPassword(ctx, "h3"); err != nil {
		t.Fatal(err)
	}
	o, err := s.GetOwner(ctx)
	if err != nil || o.Name != "eugene" || o.PasswordHash != "h3" {
		t.Fatalf("owner = %+v, %v", o, err)
	}
}

func TestProjectsAndCascade(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	p, err := s.CreateProject(ctx, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(ctx, "foo"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate project: %v", err)
	}
	if _, err := s.AddProtected(ctx, p.ID, "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	tg := &Target{ProjectID: p.ID, Name: "github", URL: "https://github.com/u/foo.git", AuthKind: AuthHTTPS, Enabled: true}
	if err := s.CreateTarget(ctx, tg); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTargetRef(ctx, tg.ID, "refs/heads/main", "abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEvent(ctx, &Event{ProjectID: p.ID, Kind: EventDiverged, Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TargetByID(ctx, tg.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("target must be deleted with project: %v", err)
	}
	refs, _ := s.TargetRefs(ctx, tg.ID)
	if len(refs) != 0 {
		t.Fatalf("target refs survived: %v", refs)
	}
}

func TestTargetUpdateForgetsRefsOnURLChange(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	p, _ := s.CreateProject(ctx, "foo")
	tg := &Target{ProjectID: p.ID, Name: "gh", URL: "https://a/x.git", AuthKind: AuthNone, Enabled: true}
	if err := s.CreateTarget(ctx, tg); err != nil {
		t.Fatal(err)
	}
	_ = s.SetTargetRef(ctx, tg.ID, "refs/heads/main", "abc")

	tg.Enabled = false
	if err := s.UpdateTarget(ctx, tg); err != nil {
		t.Fatal(err)
	}
	if refs, _ := s.TargetRefs(ctx, tg.ID); refs["refs/heads/main"] != "abc" {
		t.Fatalf("refs must stay when URL is unchanged: %v", refs)
	}
	tg.URL = "https://b/x.git"
	if err := s.UpdateTarget(ctx, tg); err != nil {
		t.Fatal(err)
	}
	if refs, _ := s.TargetRefs(ctx, tg.ID); len(refs) != 0 {
		t.Fatalf("refs must be forgotten after URL change: %v", refs)
	}

	now := time.Now()
	if err := s.SetTargetStatus(ctx, tg.ID, TargetStatus{State: StateError, LastError: "boom", NextAttemptAt: now, Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.TargetByID(ctx, tg.ID)
	if got.State != StateError || got.LastError != "boom" || got.Attempt != 2 || got.NextAttemptAt.UnixMilli() != now.UnixMilli() || !got.LastSuccessAt.IsZero() {
		t.Fatalf("status = %+v", got.TargetStatus)
	}
}

func TestWithTxRollback(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	boom := errors.New("boom")
	err := s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.CreateProject(ctx, "foo"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := s.ProjectByName(ctx, "foo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback did not happen: %v", err)
	}
}

func TestIntentsArchiveEvents(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	p, _ := s.CreateProject(ctx, "foo")
	in := &ArchiveIntent{ProjectID: p.ID, PushID: "push1", Ref: "refs/heads/x", OldObject: "a", NewObject: "b"}
	if err := s.AddArchiveIntent(ctx, in); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.PendingIntents(ctx, "push1")
	if len(pending) != 1 || pending[0].OldObject != "a" {
		t.Fatalf("pending = %+v", pending)
	}
	_ = s.FinishIntent(ctx, in.ID)
	if all, _ := s.AllPendingIntents(ctx); len(all) != 0 {
		t.Fatalf("finished intent still pending: %+v", all)
	}

	_ = s.AddArchiveRef(ctx, &ArchiveRef{ProjectID: p.ID, Source: SourcePush, OrigRef: "refs/heads/x", ArchiveRef: "refs/metagit/archive/push/heads/x/1", Object: "a"})
	_ = s.AddArchiveRef(ctx, &ArchiveRef{ProjectID: p.ID, Source: SourceTarget, TargetID: 7, TargetName: "gh", OrigRef: "refs/heads/y", ArchiveRef: "refs/metagit/archive/targets/7/heads/y/1", Object: "c"})
	list, _ := s.ListArchiveRefs(ctx, p.ID)
	if len(list) != 2 || list[0].TargetID != 7 || list[1].TargetID != 0 {
		t.Fatalf("archive = %+v", list)
	}

	for i := range 3 {
		_ = s.AddEvent(ctx, &Event{ProjectID: p.ID, Kind: EventDeliveryError, Message: string(rune('a' + i))})
	}
	events, _ := s.ListEvents(ctx, p.ID, 2)
	if len(events) != 2 || events[0].Message != "c" {
		t.Fatalf("events = %+v", events)
	}
}

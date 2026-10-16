package app_test

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/apptest"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
)

var pushArchiveName = regexp.MustCompile(`^refs/metagit/archive/push/(heads|tags)/[^ ]+/\d{8}T\d{6}Z(-\d+)?$`)

func archiveFor(list []store.ArchiveRef, ref string) []store.ArchiveRef {
	var out []store.ArchiveRef
	for _, a := range list {
		if a.OrigRef == ref {
			out = append(out, a)
		}
	}
	return out
}

// Scenarios "Архив перед разрушающим push" (ref-archive). The kind of each
// update is decided by the pre-receive hook, so the archive (or its absence)
// shows the classification of create, fast-forward, non-fast-forward,
// delete and tag move.
func TestDestructivePushCreatesArchive(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	repo := e.RepoPath("foo")
	work := testutil.InitWork(t)

	c := testutil.Commit(t, work, "c")
	testutil.Git(t, work, "branch", "feature")
	e.Push(work, "foo", "main", "feature")
	if n := len(e.Archive("foo")); n != 0 {
		t.Fatalf("create must not archive, got %d records", n)
	}

	// Fast-forward.
	testutil.Git(t, work, "checkout", "-q", "feature")
	c2 := testutil.Commit(t, work, "c2")
	e.Push(work, "foo", "feature")
	if n := len(e.Archive("foo")); n != 0 {
		t.Fatalf("fast-forward must not archive, got %d records", n)
	}

	// Force-push of a commit that does not contain the current one.
	testutil.Git(t, work, "reset", "-q", "--hard", c)
	d := testutil.Commit(t, work, "d")
	out := e.Push(work, "foo", "--force", "feature")
	list := archiveFor(e.Archive("foo"), "refs/heads/feature")
	if len(list) != 1 || list[0].Object != c2 || list[0].Source != store.SourcePush {
		t.Fatalf("archive after force-push = %+v", list)
	}
	if !pushArchiveName.MatchString(list[0].ArchiveRef) || !strings.HasPrefix(list[0].ArchiveRef, "refs/metagit/archive/push/heads/feature/") {
		t.Fatalf("archive name %q", list[0].ArchiveRef)
	}
	if got := testutil.RefOf(t, repo, list[0].ArchiveRef); got != c2 {
		t.Fatalf("archive ref points to %q, want %q", got, c2)
	}
	if !strings.Contains(out, "previous state saved in "+list[0].ArchiveRef) {
		t.Fatalf("push output does not mention the archive:\n%s", out)
	}

	// Deletion.
	e.Push(work, "foo", ":feature")
	list = archiveFor(e.Archive("foo"), "refs/heads/feature")
	if len(list) != 2 || list[0].Object != d || testutil.RefOf(t, repo, list[0].ArchiveRef) != d {
		t.Fatalf("archive after delete = %+v", list)
	}

	// Tag move.
	testutil.Git(t, work, "tag", "-a", "-m", "v1", "v1.0", c)
	tagObj := testutil.Git(t, work, "rev-parse", "v1.0")
	e.Push(work, "foo", "v1.0")
	testutil.Git(t, work, "tag", "-f", "-a", "-m", "v1 again", "v1.0", d)
	e.Push(work, "foo", "--force", "v1.0")
	list = archiveFor(e.Archive("foo"), "refs/tags/v1.0")
	if len(list) != 1 || list[0].Object != tagObj || !strings.HasPrefix(list[0].ArchiveRef, "refs/metagit/archive/push/tags/v1.0/") {
		t.Fatalf("archive after tag move = %+v", list)
	}

	// Every archive has an event in the project log.
	var archiveEvents int
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventArchivePush && ev.ArchiveRef != "" {
			archiveEvents++
		}
	}
	if archiveEvents != 3 {
		t.Fatalf("archive events = %d, want 3", archiveEvents)
	}
}

// Scenarios "Защищенные ветки и теги" and "Служебное пространство refs"
// (git-hosting).
func TestProtectedRefs(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	repo := e.RepoPath("foo")
	ctx := context.Background()
	for _, p := range []string{"refs/heads/main", "refs/tags/*"} {
		if _, err := e.App.Core.AddProtected(ctx, "foo", p); err != nil {
			t.Fatal(err)
		}
	}
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	testutil.Git(t, work, "tag", "v1.0")
	e.Push(work, "foo", "main", "v1.0")

	// Force-push to a protected branch.
	testutil.Git(t, work, "checkout", "-q", "--orphan", "other")
	x := testutil.Commit(t, work, "x")
	out, err := e.PushErr(work, "foo", "--force", "other:main")
	if err == nil || !strings.Contains(out, "refs/heads/main is protected") {
		t.Fatalf("force-push to protected main: %v\n%s", err, out)
	}
	if got := testutil.RefOf(t, repo, "refs/heads/main"); got != a {
		t.Fatalf("protected main changed to %q", got)
	}

	// Deletion of a protected branch.
	if out, err := e.PushErr(work, "foo", ":main"); err == nil || !strings.Contains(out, "deletion is not allowed") {
		t.Fatalf("delete protected main: %v\n%s", err, out)
	}

	// A rejected push changes no refs, even allowed ones.
	if out, err := e.PushErr(work, "foo", "other:feature", "--force", "other:main"); err == nil {
		t.Fatalf("mixed push must be rejected:\n%s", out)
	}
	if got := testutil.RefOf(t, repo, "refs/heads/feature"); got != "" {
		t.Fatalf("feature was created by a rejected push: %q", got)
	}

	// Moving a protected tag.
	testutil.Git(t, work, "tag", "-f", "v1.0", x)
	if out, err := e.PushErr(work, "foo", "--force", "v1.0"); err == nil || !strings.Contains(out, "refs/tags/v1.0 is protected") {
		t.Fatalf("move protected tag: %v\n%s", err, out)
	}
	if got := testutil.RefOf(t, repo, "refs/tags/v1.0"); got != a {
		t.Fatalf("protected tag moved to %q", got)
	}

	// Fast-forward to a protected branch is allowed.
	testutil.Git(t, work, "checkout", "-q", "main")
	b := testutil.Commit(t, work, "b")
	e.Push(work, "foo", "main")
	if got := testutil.RefOf(t, repo, "refs/heads/main"); got != b {
		t.Fatalf("fast-forward to protected main: %q", got)
	}

	// Force-push to an unprotected branch is allowed.
	e.Push(work, "foo", "main:feature")
	e.Push(work, "foo", "--force", "other:feature")
	if got := testutil.RefOf(t, repo, "refs/heads/feature"); got != x {
		t.Fatalf("force-push to unprotected feature: %q", got)
	}

	// The metagit namespace is closed for pushes.
	out, err = e.PushErr(work, "foo", "main:refs/metagit/archive/anything")
	if err == nil || !strings.Contains(out, "reserved namespace") {
		t.Fatalf("push to refs/metagit/: %v\n%s", err, out)
	}
	if got := testutil.RefOf(t, repo, "refs/metagit/archive/anything"); got != "" {
		t.Fatal("service ref was created")
	}
}

// Scenario "Сбой между push и архивом" (ref-archive).
func TestArchiveRecoveryAfterCrash(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	p := e.CreateProject("foo")
	repo := e.RepoPath("foo")
	work := testutil.InitWork(t)
	b := testutil.Commit(t, work, "b")
	testutil.Git(t, work, "branch", "feature")
	testutil.Git(t, work, "branch", "untouched")
	c := testutil.Commit(t, work, "c")
	e.Push(work, "foo", "main", "feature", "untouched")
	dataDir := e.DataDir
	e.Stop()

	// State after a crash: pre-receive recorded intents, git moved feature,
	// but post-receive never ran. "untouched" kept its value (failed push).
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(dataDir, "metagit.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"refs/heads/feature", "refs/heads/untouched"} {
		in := &store.ArchiveIntent{ProjectID: p.ID, PushID: "crashed", Ref: ref, OldObject: b, NewObject: c}
		if err := st.AddArchiveIntent(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	testutil.Git(t, repo, "update-ref", "refs/heads/feature", c)

	e2 := apptest.Start(t, apptest.Options{DataDir: dataDir})
	list := e2.Archive("foo")
	if len(list) != 1 || list[0].OrigRef != "refs/heads/feature" || list[0].Object != b {
		t.Fatalf("archive after recovery = %+v", list)
	}
	if got := testutil.RefOf(t, repo, list[0].ArchiveRef); got != b {
		t.Fatalf("recovered archive ref points to %q", got)
	}
	pending, err := e2.App.Store.AllPendingIntents(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending intents after recovery: %+v %v", pending, err)
	}
}

// Scenario "Восстановление из архива" (ref-archive).
func TestFetchArchiveRefOverHTTPAndSSH(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	old := testutil.Commit(t, work, "b")
	e.Push(work, "foo", "main:feature")
	testutil.Git(t, work, "reset", "-q", "--hard", "HEAD~1")
	testutil.Commit(t, work, "rewritten")
	e.Push(work, "foo", "--force", "main:feature")
	list := e.Archive("foo")
	if len(list) != 1 {
		t.Fatalf("archive = %+v", list)
	}
	name := list[0].ArchiveRef

	for _, tc := range []struct {
		proto string
		url   string
		env   []string
	}{
		{"http", e.GitURL("foo"), nil},
		{"ssh", e.SSHURL("foo"), e.SSHEnv()},
	} {
		t.Run(tc.proto, func(t *testing.T) {
			clone := testutil.InitWork(t)
			if out, err := testutil.GitErr(clone, tc.env, "fetch", tc.url, name); err != nil {
				t.Fatalf("fetch: %v\n%s", err, out)
			}
			if got := testutil.Git(t, clone, "rev-parse", "FETCH_HEAD"); got != old {
				t.Fatalf("FETCH_HEAD = %q, want %q", got, old)
			}
			if n := testutil.Git(t, clone, "rev-list", "--count", "FETCH_HEAD"); n != "2" {
				t.Fatalf("history is incomplete: %s commits", n)
			}
		})
	}
}

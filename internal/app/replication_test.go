package app_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/apptest"
	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/gitssh"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
	"github.com/ekalinin/metagit/internal/testutil/fakehost"
)

const hostToken = "host-token-123456"

// pushToHost changes the hosting repository directly, as a user of the
// hosting would do (merge in the web interface, push in bypass of metagit).
func pushToHost(t *testing.T, h *fakehost.Host, work string, refspecs ...string) {
	t.Helper()
	testutil.Git(t, work, append([]string{"push", "-q", "--force", h.Path("foo")}, refspecs...)...)
}

// Scenario "Первая доставка на новый target" (mirror-replication).
func TestFirstDeliveryToNewTarget(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	testutil.Git(t, work, "tag", "-a", "-m", "v1", "v1")
	testutil.Git(t, work, "checkout", "-q", "-b", "dev")
	d := testutil.Commit(t, work, "d")
	e.Push(work, "foo", "main", "dev", "v1")

	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitSynced(tg.ID)

	if h.Ref(t, "foo", "refs/heads/main") != a || h.Ref(t, "foo", "refs/heads/dev") != d {
		t.Fatal("branches are not delivered")
	}
	if h.Ref(t, "foo", "refs/tags/v1") != testutil.RefOf(t, e.RepoPath("foo"), "refs/tags/v1") {
		t.Fatal("tag is not delivered")
	}
	if got := e.Target(tg.ID); got.LastSuccessAt.IsZero() || got.LastError != "" {
		t.Fatalf("status after delivery: %+v", got.TargetStatus)
	}
}

// Scenario "URL с секретом" (mirror-replication).
func TestTargetURLWithSecretRejected(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	token := "abc12345"
	_, err := e.App.Core.AddTarget(context.Background(), "foo", core.TargetInput{
		Name: "gh", URL: "https://user:token@github.com/u/foo.git", AuthKind: store.AuthHTTPS, Secret: &token, Enabled: true,
	})
	ve, ok := errors.AsType[*core.ValidationError](err)
	if !ok || !strings.Contains(ve.Msg, "credentials in the URL are not allowed") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

// Scenario "Выключенный target" (mirror-replication).
func TestDisabledTargetIsNotContacted(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, false)
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	out := e.Push(work, "foo", "main")
	time.Sleep(300 * time.Millisecond)
	if h.Requests() != 0 {
		t.Fatalf("disabled target received %d requests", h.Requests())
	}
	if strings.Contains(out, "github") || !strings.Contains(out, "no enabled targets") {
		t.Fatalf("push output:\n%s", out)
	}
	if got := e.Target(tg.ID); got.State != store.StatePending {
		t.Fatalf("state = %s", got.State)
	}
}

// Credentials of HTTPS targets go through GIT_ASKPASS.
func TestDeliveryWithToken(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")

	tg := e.AddTarget("foo", "github", h.URL("foo"), "wrong-token", true)
	got := e.WaitTarget(tg.ID, "error", func(t *store.Target) bool { return t.State == store.StateError })
	if !strings.Contains(got.LastError, "Authentication failed") || strings.Contains(got.LastError, "wrong-token") {
		t.Fatalf("error = %q", got.LastError)
	}

	token := hostToken
	_, err := e.App.Core.UpdateTarget(context.Background(), "foo", tg.ID, core.TargetInput{
		Name: "github", URL: h.URL("foo"), AuthKind: store.AuthHTTPS, Username: "x", Secret: &token, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.WaitSynced(tg.ID)
	if h.Ref(t, "foo", "refs/heads/main") != a {
		t.Fatal("main is not delivered with the new token")
	}
	stored := e.Target(tg.ID)
	if strings.Contains(string(stored.Secret), hostToken) {
		t.Fatal("token is stored unencrypted")
	}
}

// Scenario "Временная ошибка хостинга" (mirror-replication) when there is
// nothing to push: a wrong token is still an error, also for a public
// repository that answers reads without the token.
func TestNothingToPushChecksToken(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	h.SetPublic(true)
	other := testutil.InitWork(t)
	testutil.Commit(t, other, "pr")
	pushToHost(t, h, other, "main:refs/heads/pr-branch")
	hostRefs := testutil.Git(t, h.Path("foo"), "for-each-ref")

	tg := e.AddTarget("foo", "github", h.URL("foo"), "wrong-token", true)
	got := e.WaitTarget(tg.ID, "several attempts", func(t *store.Target) bool { return t.State == store.StateError && t.Attempt >= 2 })
	if !strings.Contains(got.LastError, "Authentication failed") || strings.Contains(got.LastError, "wrong-token") || got.NextAttemptAt.IsZero() {
		t.Fatalf("status of the empty project: %+v", got.TargetStatus)
	}
	var errorEvents int
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventDeliveryError {
			errorEvents++
		}
	}
	if errorEvents != 1 {
		t.Fatalf("delivery error events = %d", errorEvents)
	}

	update := func(token string) {
		t.Helper()
		_, err := e.App.Core.UpdateTarget(context.Background(), "foo", tg.ID, core.TargetInput{
			Name: "github", URL: h.URL("foo"), AuthKind: store.AuthHTTPS, Username: "x", Secret: &token, Enabled: true,
		})
		apptest.Must(t, err)
	}
	update(hostToken)
	e.WaitSynced(tg.ID)
	if got := testutil.Git(t, h.Path("foo"), "for-each-ref"); got != hostRefs {
		t.Fatalf("hosting changed:\n%s\nwas:\n%s", got, hostRefs)
	}

	// Everything is delivered, then the token becomes wrong.
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")
	e.WaitSynced(tg.ID)
	update("wrong-token")
	got = e.WaitTarget(tg.ID, "error", func(t *store.Target) bool { return t.State == store.StateError })
	if !strings.Contains(got.LastError, "Authentication failed") {
		t.Fatalf("error = %q", got.LastError)
	}
}

// Scenario "Временная ошибка хостинга" (mirror-replication) over SSH when
// there is nothing to push: an unknown key and a read-only key are errors.
func TestNothingToPushChecksSSHKey(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")

	// An SSH hosting with an empty repository. While the read-only file
	// exists, it refuses to receive pushes, as hostings do for read-only keys.
	dir := t.TempDir()
	testutil.Git(t, dir, "init", "-q", "--bare", "-b", "main", filepath.Join(dir, "foo.git"))
	readOnly := filepath.Join(dir, "read-only")
	gitWrapper := filepath.Join(dir, "git-wrapper")
	script := "#!/bin/sh\nif [ \"$1\" = receive-pack ] && [ -e " + readOnly + " ]; then\n" +
		"\techo 'ERROR: The key you are authenticating with has been marked as read only.' >&2\n\texit 1\nfi\nexec git \"$@\"\n"
	apptest.Must(t, os.WriteFile(gitWrapper, []byte(script), 0o755))
	newKey := func() (string, ssh.PublicKey) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		block, _ := ssh.MarshalPrivateKey(priv, "")
		signer, _ := ssh.NewSignerFromKey(priv)
		return string(pem.EncodeToMemory(block)), signer.PublicKey()
	}
	deployKey, deployPub := newKey()
	unknownKey, _ := newKey()
	hostKey, err := gitssh.LoadOrCreateHostKey(filepath.Join(dir, "host_key"))
	apptest.Must(t, err)
	srv := &gitssh.Server{
		ReposDir: dir,
		Git:      gitWrapper,
		HostKey:  hostKey,
		CheckKey: func(_ context.Context, k ssh.PublicKey) bool {
			return string(k.Marshal()) == string(deployPub.Marshal())
		},
		ProjectExists: func(context.Context, string) bool { return true },
		Pushes:        noPushes{},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	apptest.Must(t, err)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })

	ctx := context.Background()
	input := func(key string) core.TargetInput {
		return core.TargetInput{Name: "ssh-host", URL: "ssh://git@" + l.Addr().String() + "/foo.git", AuthKind: store.AuthSSH, Secret: &key, Enabled: true}
	}
	failedWith := func(text string) func(*store.Target) bool {
		return func(t *store.Target) bool { return t.State == store.StateError && strings.Contains(t.LastError, text) }
	}
	tg, err := e.App.Core.AddTarget(ctx, "foo", input(unknownKey))
	apptest.Must(t, err)
	e.WaitTarget(tg.ID, "unknown key error", failedWith("Permission denied"))

	apptest.Must(t, os.WriteFile(readOnly, nil, 0o600))
	_, err = e.App.Core.UpdateTarget(ctx, "foo", tg.ID, input(deployKey))
	apptest.Must(t, err)
	e.WaitTarget(tg.ID, "read-only key error", failedWith("marked as read only"))

	apptest.Must(t, os.Remove(readOnly))
	apptest.Must(t, e.App.Core.SyncTarget(ctx, "foo", tg.ID))
	e.WaitSynced(tg.ID)
	if refs := testutil.Git(t, filepath.Join(dir, "foo.git"), "for-each-ref"); refs != "" {
		t.Fatalf("refs created on the hosting:\n%s", refs)
	}
}

// SSH targets use a temporary key file that is removed after delivery.
func TestSSHDeliveryRemovesKeyFile(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")

	// An SSH hosting: the git SSH server of metagit over a plain repository.
	dir := t.TempDir()
	hostRepos := &repo.Manager{ReposDir: dir, HooksDir: filepath.Join(dir, "no-hooks"), Git: &gitcmd.Runner{}}
	if err := hostRepos.Create(context.Background(), "foo"); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	signer, _ := ssh.NewSignerFromKey(priv)
	hostKey, err := gitssh.LoadOrCreateHostKey(filepath.Join(dir, "host_key"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &gitssh.Server{
		ReposDir: dir,
		HostKey:  hostKey,
		CheckKey: func(_ context.Context, k ssh.PublicKey) bool {
			return string(k.Marshal()) == string(signer.PublicKey().Marshal())
		},
		ProjectExists: func(_ context.Context, name string) bool { return hostRepos.Exists(name) },
		Pushes:        noPushes{},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })

	key := string(pem.EncodeToMemory(block))
	tg, err := e.App.Core.AddTarget(context.Background(), "foo", core.TargetInput{
		Name: "ssh-host", URL: "ssh://git@" + l.Addr().String() + "/foo.git", AuthKind: store.AuthSSH, Secret: &key, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.WaitSynced(tg.ID)
	if got := testutil.RefOf(t, hostRepos.Path("foo"), "refs/heads/main"); got != a {
		t.Fatalf("main on SSH host = %q", got)
	}
	entries, err := os.ReadDir(filepath.Join(e.DataDir, config.TmpDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files left: %v", entries)
	}
}

type noPushes struct{}

func (noPushes) NewPush(string) ([]string, func()) { return nil, func() {} }

// Scenarios "Ветка, созданная только на хостинге", "Удаление ветки в
// metagit", "Прочие пространства refs" and "Force-push в незащищенную ветку".
func TestDeliveryTouchesOnlyMetagitRefs(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	testutil.Git(t, work, "branch", "old")
	testutil.Git(t, work, "branch", "feature")
	e.Push(work, "foo", "main", "old", "feature")

	// A branch that exists only on the hosting.
	other := testutil.InitWork(t)
	pr := testutil.Commit(t, other, "pr")
	pushToHost(t, h, other, "main:refs/heads/pr-branch")

	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitSynced(tg.ID)
	if h.Ref(t, "foo", "refs/heads/old") != a {
		t.Fatal("old is not delivered")
	}

	// Delete "old", rewrite "feature", add a note (a ref outside heads and tags).
	testutil.Git(t, work, "checkout", "-q", "--orphan", "rewrite")
	x := testutil.Commit(t, work, "x")
	testutil.Git(t, work, "notes", "add", "-m", "note", x)
	out := e.Push(work, "foo", ":old", "--force", "rewrite:feature", "refs/notes/commits")
	if !strings.Contains(out, "github: delivered") {
		t.Fatalf("push output:\n%s", out)
	}
	if h.Ref(t, "foo", "refs/heads/old") != "" {
		t.Fatal("old is not deleted on the hosting")
	}
	if h.Ref(t, "foo", "refs/heads/feature") != x {
		t.Fatal("force-push is not delivered")
	}
	if h.Ref(t, "foo", "refs/heads/pr-branch") != pr {
		t.Fatal("branch created on the hosting was touched")
	}
	if h.Ref(t, "foo", "refs/notes/commits") != "" {
		t.Fatal("refs outside heads and tags must not be delivered")
	}
	refs := testutil.Git(t, h.Path("foo"), "for-each-ref", "--format=%(refname)")
	if strings.Contains(refs, "refs/metagit/") {
		t.Fatalf("metagit refs leaked to the hosting:\n%s", refs)
	}
}

var targetArchiveName = regexp.MustCompile(`^refs/metagit/archive/targets/\d+/heads/main/\d{8}T\d{6}Z(-\d+)?$`)

// Scenario "Merge в веб-интерфейсе хостинга" (mirror-replication) and
// "Коммит, сделанный на хостинге" (ref-archive).
func TestDivergedTargetIsArchivedAndOverwritten(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "c")
	e.Push(work, "foo", "main")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitSynced(tg.ID)

	// Somebody merges on the hosting: main becomes E (child of C).
	other := filepath.Join(t.TempDir(), "other")
	testutil.Git(t, t.TempDir(), "clone", "-q", h.Path("foo"), other)
	eCommit := testutil.Commit(t, other, "e on hosting")
	pushToHost(t, h, other, "main")

	// The owner pushes D (child of C, not of E) to metagit.
	dCommit := testutil.Commit(t, work, "d")
	out := e.Push(work, "foo", "main")

	if got := h.Ref(t, "foo", "refs/heads/main"); got != dCommit {
		t.Fatalf("main on hosting = %q, want D %q", got, dCommit)
	}
	list := e.Archive("foo")
	if len(list) != 1 || list[0].Source != store.SourceTarget || list[0].Object != eCommit || list[0].TargetName != "github" {
		t.Fatalf("archive = %+v", list)
	}
	if !targetArchiveName.MatchString(list[0].ArchiveRef) {
		t.Fatalf("archive name %q", list[0].ArchiveRef)
	}
	if got := testutil.RefOf(t, e.RepoPath("foo"), list[0].ArchiveRef); got != eCommit {
		t.Fatalf("archive ref points to %q", got)
	}
	if !strings.Contains(out, "github: delivered; diverged") || !strings.Contains(out, list[0].ArchiveRef) {
		t.Fatalf("push output:\n%s", out)
	}
	var diverged []store.Event
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventDiverged {
			diverged = append(diverged, ev)
		}
	}
	if len(diverged) != 1 || diverged[0].TargetName != "github" || diverged[0].Ref != "refs/heads/main" || diverged[0].ArchiveRef != list[0].ArchiveRef {
		t.Fatalf("diverged events = %+v", diverged)
	}

	// E is available from metagit with its history.
	clone := testutil.InitWork(t)
	testutil.Git(t, clone, "fetch", "-q", e.GitURL("foo"), list[0].ArchiveRef)
	if testutil.Git(t, clone, "rev-parse", "FETCH_HEAD") != eCommit {
		t.Fatal("E cannot be fetched from metagit")
	}
	_ = c
}

// Scenarios "Совпадающая ветка с таким же именем" and "Ветка на хостинге
// отстает" (mirror-replication).
func TestMatchingOrBehindRefsAreNotDivergence(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	b := testutil.Commit(t, work, "b")
	testutil.Git(t, work, "branch", "same")
	c := testutil.Commit(t, work, "c")
	e.Push(work, "foo", "main", "same")

	// The hosting already has "same" = B (equal) and "main" = B (behind C).
	pushToHost(t, h, work, b+":refs/heads/same", b+":refs/heads/main")

	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitSynced(tg.ID)
	if h.Ref(t, "foo", "refs/heads/main") != c || h.Ref(t, "foo", "refs/heads/same") != b {
		t.Fatal("refs are not delivered")
	}
	if list := e.Archive("foo"); len(list) != 0 {
		t.Fatalf("no archive expected, got %+v", list)
	}
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventDiverged {
			t.Fatalf("unexpected divergence event: %+v", ev)
		}
	}
}

// A tag that differs on the hosting is archived and overwritten too.
func TestDivergedTag(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	b := testutil.Commit(t, work, "b")
	testutil.Git(t, work, "tag", "v1", b)
	e.Push(work, "foo", "main", "v1")
	pushToHost(t, h, work, a+":refs/tags/v1")

	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitSynced(tg.ID)
	if h.Ref(t, "foo", "refs/tags/v1") != b {
		t.Fatal("tag is not overwritten")
	}
	list := e.Archive("foo")
	if len(list) != 1 || list[0].OrigRef != "refs/tags/v1" || list[0].Object != a {
		t.Fatalf("archive = %+v", list)
	}
}

// Scenario "Несколько push подряд" (mirror-replication).
func TestSeveralPushesEndWithLatestState(t *testing.T) {
	e := apptest.Start(t, apptest.Options{PushStatusTimeout: 50 * time.Millisecond})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	h.SetDelay(150 * time.Millisecond)

	work := testutil.InitWork(t)
	var last string
	for i := range 3 {
		last = testutil.Commit(t, work, "c"+string(rune('0'+i)))
		e.Push(work, "foo", "main")
	}
	e.WaitTarget(tg.ID, "latest main", func(*store.Target) bool { return h.Ref(t, "foo", "refs/heads/main") == last })
	e.WaitSynced(tg.ID)
}

// Scenarios "Временная ошибка хостинга" and "Хостинг снова доступен"
// (mirror-replication).
func TestRetriesUntilHostIsBack(t *testing.T) {
	e := apptest.Start(t, apptest.Options{RetryMin: 100 * time.Millisecond, RetryMax: 300 * time.Millisecond})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	h.SetDown(true)
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)

	failed := e.WaitTarget(tg.ID, "several attempts", func(t *store.Target) bool { return t.State == store.StateError && t.Attempt >= 2 })
	if !strings.Contains(failed.LastError, "503") || failed.NextAttemptAt.IsZero() {
		t.Fatalf("error status: %+v", failed.TargetStatus)
	}
	var errorEvents int
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventDeliveryError {
			errorEvents++
		}
	}
	if errorEvents != 1 {
		t.Fatalf("repeated identical errors must give one event, got %d", errorEvents)
	}

	h.SetDown(false)
	synced := e.WaitSynced(tg.ID)
	if h.Ref(t, "foo", "refs/heads/main") != a || synced.Attempt != 0 || synced.LastError != "" {
		t.Fatalf("after recovery: %+v", synced.TargetStatus)
	}
}

// Scenario "Ручная доставка" (web-console): no waiting for the retry timer.
func TestManualSyncDoesNotWaitForRetry(t *testing.T) {
	e := apptest.Start(t, apptest.Options{RetryMin: time.Hour, RetryMax: time.Hour})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	h.SetDown(true)
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitTarget(tg.ID, "error", func(t *store.Target) bool { return t.State == store.StateError })

	h.SetDown(false)
	if err := e.App.Core.SyncTarget(context.Background(), "foo", tg.ID); err != nil {
		t.Fatal(err)
	}
	e.WaitSynced(tg.ID)
}

// Scenarios "Все target'ы ответили быстро", "Медленный target" and "Ошибка
// доставки в выводе push" (mirror-replication).
func TestPushOutputShowsDeliveryStatus(t *testing.T) {
	e := apptest.Start(t, apptest.Options{PushStatusTimeout: 1500 * time.Millisecond})
	e.CreateProject("foo")
	fast := fakehost.New(t, hostToken)
	fast.CreateRepo(t, "foo")
	slow := fakehost.New(t, hostToken)
	slow.CreateRepo(t, "foo")
	broken := fakehost.New(t, hostToken)
	broken.CreateRepo(t, "foo")
	e.AddTarget("foo", "fast", fast.URL("foo"), hostToken, true)
	slowTarget := e.AddTarget("foo", "slow", slow.URL("foo"), hostToken, true)
	e.AddTarget("foo", "broken", broken.URL("foo"), "bad-token", true)
	time.Sleep(500 * time.Millisecond) // initial deliveries of the empty project
	slow.SetDelay(2 * time.Second)

	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	start := time.Now()
	out := e.Push(work, "foo", "main")
	elapsed := time.Since(start)

	for _, want := range []string{"metagit: fast: delivered", "metagit: slow: queued", "metagit: broken: error:"} {
		if !strings.Contains(out, want) {
			t.Errorf("push output has no %q:\n%s", want, out)
		}
	}
	// The error line has a short error text and the retry mark.
	if !regexp.MustCompile(`(?m)metagit: broken: error: \S.* \(retry later\)[ \t]*$`).MatchString(out) {
		t.Errorf("push output has no error line with %q for broken:\n%s", "(retry later)", out)
	}
	if elapsed > 4*time.Second {
		t.Errorf("push waited %v, the timeout is 1.5s", elapsed)
	}
	if fast.Ref(t, "foo", "refs/heads/main") != a {
		t.Error("fast target is not delivered")
	}
	slow.SetDelay(0)
	e.WaitTarget(slowTarget.ID, "delivery in background", func(*store.Target) bool { return slow.Ref(t, "foo", "refs/heads/main") == a })
}

// Scenario "Ошибка доставки с токеном в URL запроса" (mirror-replication).
func TestSecretsAreMaskedInErrors(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	repoPath := h.CreateRepo(t, "foo")
	// The hosting rejects the push and echoes the token back.
	hook := "#!/bin/sh\necho \"denied for token " + hostToken + "\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(repoPath, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)

	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	out := e.Push(work, "foo", "main")
	if strings.Contains(out, hostToken) || !strings.Contains(out, "github: error:") {
		t.Fatalf("push output:\n%s", out)
	}
	var found bool
	for _, ev := range e.Events("foo") {
		if strings.Contains(ev.Message, hostToken) {
			t.Fatalf("token in event: %q", ev.Message)
		}
		if ev.Kind == store.EventDeliveryError && strings.Contains(ev.Message, "denied for token ***") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no masked delivery error event: %+v", e.Events("foo"))
	}
	targets, _ := e.App.Core.Targets(context.Background(), "foo")
	if strings.Contains(targets[0].LastError, hostToken) || !strings.Contains(targets[0].LastError, "***") {
		t.Fatalf("last error = %q", targets[0].LastError)
	}
}

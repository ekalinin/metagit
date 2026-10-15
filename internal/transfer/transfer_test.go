package transfer_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ekalinin/metagit/internal/apptest"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
	"github.com/ekalinin/metagit/internal/testutil/fakehost"
	"github.com/ekalinin/metagit/internal/transfer"
)

const (
	hostToken  = "host-token-123456"
	passphrase = "export-passphrase"
)

// source creates an instance with two projects, protected refs and targets.
func source(t *testing.T, h *fakehost.Host) *apptest.Env {
	t.Helper()
	e := apptest.Start(t, apptest.Options{})
	ctx := context.Background()
	e.CreateProject("foo")
	e.CreateProject("bar")
	_, err := e.App.Core.AddProtected(ctx, "foo", "refs/heads/main")
	apptest.Must(t, err)
	_, err = e.App.Core.AddProtected(ctx, "foo", "refs/tags/*")
	apptest.Must(t, err)
	e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.AddTarget("foo", "disabled", h.URL("other"), hostToken, false)
	return e
}

func export(t *testing.T, e *apptest.Env) []byte {
	t.Helper()
	data, err := e.App.Transfer.Export(context.Background(), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExport(t *testing.T) {
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	e := source(t, h)
	data := export(t, e)

	if bytes.Contains(data, []byte(hostToken)) || bytes.Contains(data, []byte("github")) || bytes.Contains(data, []byte("foo")) {
		t.Fatal("the export file is not encrypted")
	}
	if !bytes.HasPrefix(data, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) {
		t.Fatalf("unexpected format: %.40q", data)
	}
	if _, err := transfer.Decrypt(data, "wrong-passphrase"); err == nil {
		t.Fatal("decrypted with a wrong passphrase")
	}
	settings, err := transfer.Decrypt(data, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Version != transfer.Version || len(settings.Projects) != 2 {
		t.Fatalf("settings = %+v", settings)
	}
	foo := settings.Projects[slices.IndexFunc(settings.Projects, func(p transfer.Project) bool { return p.Name == "foo" })]
	if !slices.Equal(foo.ProtectedRefs, []string{"refs/heads/main", "refs/tags/*"}) {
		t.Fatalf("protected refs = %v", foo.ProtectedRefs)
	}
	if len(foo.Targets) != 2 {
		t.Fatalf("targets = %+v", foo.Targets)
	}
	for _, tg := range foo.Targets {
		if tg.Secret != hostToken || tg.AuthKind != store.AuthHTTPS {
			t.Fatalf("target credentials are not exported: %+v", tg)
		}
	}

	if _, err := e.App.Transfer.Export(context.Background(), "short"); err == nil {
		t.Fatal("short passphrase must be rejected")
	}
}

// Scenarios "Восстановление на новом сервере", "Импорт без push" and
// "Заполнение проекта из локальной копии" (settings-transfer).
func TestImportRestoresSettingsAndKeepsHostingsSafe(t *testing.T) {
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	src := source(t, h)
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "c")
	testutil.Git(t, work, "branch", "only-on-hosting")
	src.Push(work, "foo", "main", "only-on-hosting")
	targets, _ := src.App.Core.Targets(context.Background(), "foo")
	for _, tg := range targets {
		if tg.Enabled {
			src.WaitSynced(tg.ID)
		}
	}
	data := export(t, src)
	src.Stop()
	hostRefsBefore := testutil.Git(t, h.Path("foo"), "for-each-ref")

	dst := apptest.Start(t, apptest.Options{})
	ctx := context.Background()
	created, err := dst.App.Transfer.Import(ctx, data, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(created)
	if !slices.Equal(created, []string{"bar", "foo"}) {
		t.Fatalf("created = %v", created)
	}
	for _, name := range created {
		if !dst.App.Repos.Exists(name) {
			t.Fatalf("repository %s is missing", name)
		}
		if refs := testutil.Git(t, dst.RepoPath(name), "for-each-ref"); refs != "" {
			t.Fatalf("imported repository %s is not empty: %s", name, refs)
		}
	}
	protected, _ := dst.App.Core.Protected(ctx, "foo")
	if len(protected) != 2 {
		t.Fatalf("protected = %+v", protected)
	}
	imported, _ := dst.App.Core.Targets(ctx, "foo")
	if len(imported) != 2 {
		t.Fatalf("targets = %+v", imported)
	}
	var github store.Target
	for _, tg := range imported {
		if tg.Name == "github" {
			github = tg
		}
		if tg.Name == "disabled" && tg.Enabled {
			t.Fatal("enabled flag is not restored")
		}
	}
	if github.URL != h.URL("foo") || len(github.Secret) == 0 || bytes.Contains(github.Secret, []byte(hostToken)) {
		t.Fatalf("github target = %+v", github)
	}

	// Nothing changes on the hosting until a push arrives.
	time.Sleep(500 * time.Millisecond)
	if got := testutil.Git(t, h.Path("foo"), "for-each-ref"); got != hostRefsBefore {
		t.Fatalf("hosting changed after import:\n%s\nwas:\n%s", got, hostRefsBefore)
	}

	// The owner fills the project from a local copy: main matches the hosting.
	out := dst.Push(work, "foo", "main")
	if !strings.Contains(out, "github: доставлено") || strings.Contains(out, "расхождение") {
		t.Fatalf("push output:\n%s", out)
	}
	dst.WaitSynced(github.ID)
	if h.Ref(t, "foo", "refs/heads/main") != c || h.Ref(t, "foo", "refs/heads/only-on-hosting") != c {
		t.Fatal("refs on the hosting changed")
	}
	if list := dst.Archive("foo"); len(list) != 0 {
		t.Fatalf("no archive expected: %+v", list)
	}
	for _, ev := range dst.Events("foo") {
		if ev.Kind == store.EventDiverged {
			t.Fatalf("unexpected divergence: %+v", ev)
		}
	}
}

// Scenarios "Неверный пароль" and "Конфликт имен" (settings-transfer).
func TestImportIsAllOrNothing(t *testing.T) {
	h := fakehost.New(t, hostToken)
	src := source(t, h)
	data := export(t, src)

	dst := apptest.Start(t, apptest.Options{})
	ctx := context.Background()

	_, err := dst.App.Transfer.Import(ctx, data, "wrong-passphrase")
	if ve, ok := errors.AsType[*core.ValidationError](err); !ok || !strings.Contains(ve.Msg, "неверный пароль") {
		t.Fatalf("wrong passphrase: %v", err)
	}
	damaged := bytes.Replace(data, []byte("\n"), []byte("\nAAAA"), 3)
	if _, err := dst.App.Transfer.Import(ctx, damaged, passphrase); err == nil {
		t.Fatal("damaged file accepted")
	}

	dst.CreateProject("bar")
	_, err = dst.App.Transfer.Import(ctx, data, passphrase)
	ve, ok := errors.AsType[*core.ValidationError](err)
	if !ok || !strings.Contains(ve.Msg, "проекты уже существуют: bar") {
		t.Fatalf("conflict: %v", err)
	}

	projects, _ := dst.App.Store.ListProjects(ctx)
	if len(projects) != 1 || projects[0].Name != "bar" || dst.App.Repos.Exists("foo") {
		t.Fatalf("a failed import changed something: %+v", projects)
	}
	targets, _ := dst.App.Store.ListAllTargets(ctx)
	if len(targets) != 0 {
		t.Fatalf("targets were created: %+v", targets)
	}
}

package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ekalinin/metagit/internal/app"
	"github.com/ekalinin/metagit/internal/apptest"
	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
	"github.com/ekalinin/metagit/internal/testutil/fakehost"
)

// notification is what the test notification command received.
type notification struct {
	Event      string   `json:"event"`
	Time       string   `json:"time"`
	Project    string   `json:"project"`
	Target     string   `json:"target"`
	Ref        string   `json:"ref"`
	ArchiveRef string   `json:"archive_ref"`
	Refs       []string `json:"refs"`
	Message    string   `json:"message"`
	// Env holds METAGIT_* variables and NOTIFY_TEST_VAR seen by the command.
	Env map[string]string `json:"-"`
}

var notifyEnvVars = []string{"METAGIT_EVENT", "METAGIT_PROJECT", "METAGIT_TARGET", "METAGIT_REFS", "METAGIT_SECRET_KEY", "NOTIFY_TEST_VAR"}

// notifyCommand writes a notification command that appends the JSON from
// stdin and the variables from notifyEnvVars to files in a temporary
// directory. extra runs before that.
func notifyCommand(t *testing.T, extra string) (cmd, dir string) {
	t.Helper()
	dir = t.TempDir()
	script := "#!/bin/sh\nset -eu\n" + extra + "\n" +
		"body=$(cat)\n" +
		"printf '%s\\n' \"$body\" >> '" + filepath.Join(dir, "events") + "'\n"
	for _, v := range notifyEnvVars {
		script += "printf '%s=%s\\n' " + v + " \"${" + v + "-unset}\" >> '" + filepath.Join(dir, "env") + "'\n"
	}
	cmd = filepath.Join(dir, "notify.sh")
	if err := os.WriteFile(cmd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return cmd, dir
}

// notifications reads what the command received. Call it after Env.Stop,
// which waits for queued notifications.
func notifications(t *testing.T, dir string) []notification {
	t.Helper()
	events, err := os.ReadFile(filepath.Join(dir, "events"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(filepath.Join(dir, "env"))
	if err != nil {
		t.Fatal(err)
	}
	envLines := strings.Split(strings.TrimSuffix(string(env), "\n"), "\n")
	var list []notification
	for i, line := range strings.Split(strings.TrimSuffix(string(events), "\n"), "\n") {
		var n notification
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			t.Fatalf("notification %d: %v\n%s", i, err, line)
		}
		n.Env = map[string]string{}
		for _, kv := range envLines[i*len(notifyEnvVars) : (i+1)*len(notifyEnvVars)] {
			k, v, _ := strings.Cut(kv, "=")
			n.Env[k] = v
		}
		list = append(list, n)
	}
	return list
}

func kinds(list []notification) []string {
	var out []string
	for _, n := range list {
		out = append(out, n.Event+" "+n.Target)
	}
	return out
}

// Scenario "Неверный путь" (notifications).
func TestNotifyCommandMustBeExecutable(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.sh")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.sh"), plain} {
		dataDir := filepath.Join(t.TempDir(), "data")
		_, err := app.New(t.Context(), app.Options{
			Config: &config.Config{DataDir: dataDir, NotifyCommand: path},
			Binary: testutil.Binary(t),
		})
		if err == nil || !strings.Contains(err.Error(), "--notify-command") || !strings.Contains(err.Error(), path) {
			t.Errorf("app.New with --notify-command %s: %v", path, err)
		}
		if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
			t.Errorf("data dir is created before the check: %v", err)
		}
	}
}

// Scenarios "Ошибка доставки", "Восстановление", "Нечего доставлять",
// "Данные доставки" and "Ключ не передается" (notifications).
func TestNotifyDeliveryErrorAndRecovery(t *testing.T) {
	t.Setenv("METAGIT_SECRET_KEY", "process-secret-key")
	t.Setenv("NOTIFY_TEST_VAR", "visible")
	cmd, dir := notifyCommand(t, "")
	e := apptest.Start(t, apptest.Options{NotifyCommand: cmd})
	e.CreateProject("app")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "app")
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	e.Push(work, "app", "main")

	// Several attempts with the same error, and a push while the target fails.
	tg := e.AddTarget("app", "github", h.URL("app"), "wrong-token", true)
	e.WaitTarget(tg.ID, "several attempts", func(t *store.Target) bool { return t.State == store.StateError && t.Attempt >= 3 })
	testutil.Git(t, work, "branch", "dev")
	e.Push(work, "app", "dev")
	e.WaitTarget(tg.ID, "more attempts", func(t *store.Target) bool { return t.Attempt >= 5 })

	_, err := e.App.Core.UpdateTarget(t.Context(), "app", tg.ID, core.TargetInput{
		Name: "github", URL: h.URL("app"), AuthKind: store.AuthHTTPS, Username: "x", Secret: new(hostToken), Enabled: true,
	})
	apptest.Must(t, err)
	synced := e.WaitSynced(tg.ID)

	// Nothing to deliver.
	apptest.Must(t, e.App.Core.SyncTarget(t.Context(), "app", tg.ID))
	e.WaitTarget(tg.ID, "manual sync", func(t *store.Target) bool { return t.LastSuccessAt.After(synced.LastSuccessAt) })

	e.Stop()
	list := notifications(t, dir)
	if got := kinds(list); !slices.Equal(got, []string{"delivery_error github", "recovered github", "delivered github"}) {
		t.Fatalf("notifications: %q", got)
	}
	for _, n := range list {
		if n.Project != "app" || n.Env["METAGIT_PROJECT"] != "app" || n.Env["METAGIT_TARGET"] != "github" || n.Env["METAGIT_EVENT"] != n.Event {
			t.Errorf("notification %s: %+v", n.Event, n)
		}
		if _, err := time.Parse(time.RFC3339, n.Time); err != nil {
			t.Errorf("time of %s: %v", n.Event, err)
		}
		if n.Env["METAGIT_SECRET_KEY"] != "unset" || n.Env["NOTIFY_TEST_VAR"] != "visible" {
			t.Errorf("environment of %s: %v", n.Event, n.Env)
		}
	}
	if msg := list[0].Message; !strings.HasPrefix(msg, "delivery error: ") || !strings.Contains(msg, "Authentication failed") || strings.Contains(msg, "wrong-token") {
		t.Errorf("delivery error message: %q", msg)
	}
	if list[1].Message != "delivery to github works again" || len(list[1].Refs) != 0 {
		t.Errorf("recovered: %+v", list[1])
	}
	delivered := list[2]
	if !slices.Equal(delivered.Refs, []string{"refs/heads/dev", "refs/heads/main"}) ||
		delivered.Env["METAGIT_REFS"] != "refs/heads/dev refs/heads/main" || delivered.Message != "delivered 2 refs to github" {
		t.Errorf("delivered: %+v", delivered)
	}
}

// Scenario "Ошибка доставки с токеном в URL запроса" (mirror-replication).
func TestNotifyMasksSecrets(t *testing.T) {
	cmd, dir := notifyCommand(t, "")
	e := apptest.Start(t, apptest.Options{NotifyCommand: cmd})
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
	e.Push(work, "foo", "main")

	e.Stop()
	list := notifications(t, dir)
	if len(list) == 0 || list[0].Event != "delivery_error" {
		t.Fatalf("notifications: %q", kinds(list))
	}
	if msg := list[0].Message; strings.Contains(msg, hostToken) || !strings.Contains(msg, "denied for token ***") {
		t.Fatalf("message: %q", msg)
	}
}

// Scenario "Расхождение" (notifications).
func TestNotifyDiverged(t *testing.T) {
	cmd, dir := notifyCommand(t, "")
	e := apptest.Start(t, apptest.Options{NotifyCommand: cmd})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "c")
	e.Push(work, "foo", "main")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	e.WaitSynced(tg.ID)

	other := filepath.Join(t.TempDir(), "other")
	testutil.Git(t, t.TempDir(), "clone", "-q", h.Path("foo"), other)
	testutil.Commit(t, other, "e on hosting")
	pushToHost(t, h, other, "main")
	testutil.Commit(t, work, "d")
	e.Push(work, "foo", "main")

	var logged store.Event
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventDiverged {
			logged = ev
		}
	}
	e.Stop()
	list := notifications(t, dir)
	if got := kinds(list); !slices.Equal(got, []string{"delivered github", "diverged github", "delivered github"}) {
		t.Fatalf("notifications: %q", got)
	}
	n := list[1]
	if n.Ref != "refs/heads/main" || n.ArchiveRef == "" || n.ArchiveRef != logged.ArchiveRef || n.Message != logged.Message {
		t.Fatalf("diverged: %+v\nevent: %+v", n, logged)
	}
}

// Scenario "Force push в ветку" (notifications).
func TestNotifyArchivePush(t *testing.T) {
	cmd, dir := notifyCommand(t, "")
	e := apptest.Start(t, apptest.Options{NotifyCommand: cmd})
	e.CreateProject("foo")
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "c")
	testutil.Git(t, work, "checkout", "-q", "-b", "feature")
	testutil.Commit(t, work, "c2")
	e.Push(work, "foo", "main", "feature")
	testutil.Git(t, work, "reset", "-q", "--hard", c)
	testutil.Commit(t, work, "d")
	e.Push(work, "foo", "--force", "feature")

	var logged store.Event
	for _, ev := range e.Events("foo") {
		if ev.Kind == store.EventArchivePush {
			logged = ev
		}
	}
	e.Stop()
	list := notifications(t, dir)
	if got := kinds(list); !slices.Equal(got, []string{"archive_push "}) {
		t.Fatalf("notifications: %q", got)
	}
	n := list[0]
	if n.Ref != "refs/heads/feature" || n.ArchiveRef == "" || n.ArchiveRef != logged.ArchiveRef || n.Message != logged.Message ||
		n.Env["METAGIT_TARGET"] != "" {
		t.Fatalf("archive_push: %+v\nevent: %+v", n, logged)
	}
}

// Scenario "Медленная команда" (notifications): pushes and deliveries do not
// wait for the command.
func TestNotifyDoesNotDelayPush(t *testing.T) {
	cmd, _ := notifyCommand(t, "exec sleep 30")
	e := apptest.Start(t, apptest.Options{NotifyCommand: cmd})
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	work := testutil.InitWork(t)
	for i := range 2 {
		testutil.Commit(t, work, "c"+string(rune('0'+i)))
		start := time.Now()
		out := e.Push(work, "foo", "main")
		if d := time.Since(start); d > 10*time.Second || !strings.Contains(out, "github: delivered") {
			t.Fatalf("push %d took %s:\n%s", i, d, out)
		}
	}
}

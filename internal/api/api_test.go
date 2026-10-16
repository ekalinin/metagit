package api_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/apptest"
	"github.com/ekalinin/metagit/internal/auth"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
	"github.com/ekalinin/metagit/internal/testutil/fakehost"
)

const hostToken = "host-token-123456"

type target struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	AuthKind      string `json:"auth_kind"`
	HasSecret     bool   `json:"has_secret"`
	Enabled       bool   `json:"enabled"`
	State         string `json:"state"`
	LastError     string `json:"last_error"`
	LastSuccessAt string `json:"last_success_at"`
}

type project struct {
	Name    string   `json:"name"`
	HTTPURL string   `json:"http_url"`
	SSHURL  string   `json:"ssh_url"`
	Targets []target `json:"targets"`
}

// Scenarios "Запрос без сессии", "Неверный пароль" and "Сервер без
// владельца" (web-console).
func TestLoginAndSession(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	c := e.Client()

	if status, _ := c.Do("GET", "/api/projects", nil); status != http.StatusUnauthorized {
		t.Fatalf("projects without session: %d", status)
	}
	status, body := c.Do("POST", "/api/login", map[string]string{"name": apptest.OwnerName, "password": "wrong-password"})
	if status != http.StatusUnauthorized || !strings.Contains(body, "invalid name or password") {
		t.Fatalf("wrong password: %d %s", status, body)
	}
	if status, _ := c.Do("GET", "/api/projects", nil); status != http.StatusUnauthorized {
		t.Fatal("a failed login must not create a session")
	}

	res, _ := c.Raw("POST", "/api/login", "application/json",
		fmt.Sprintf(`{"name":%q,"password":%q}`, apptest.OwnerName, apptest.OwnerPassword))
	if res.StatusCode != 200 {
		t.Fatalf("login: %d", res.StatusCode)
	}
	cookie := res.Cookies()[0]
	if cookie.Name != auth.SessionCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %+v", cookie)
	}
	status, body = c.Do("GET", "/api/session", nil)
	if status != 200 || !strings.Contains(body, `"authenticated":true`) || !strings.Contains(body, `"owner_name":"eugene"`) {
		t.Fatalf("session: %d %s", status, body)
	}
	if status, _ := c.Do("GET", "/api/projects", nil); status != 200 {
		t.Fatalf("projects with session: %d", status)
	}
	if status, _ := c.Do("POST", "/api/logout", nil); status != 200 {
		t.Fatal("logout failed")
	}
	if status, _ := c.Do("GET", "/api/projects", nil); status != http.StatusUnauthorized {
		t.Fatal("session must end after logout")
	}
}

func TestServerWithoutOwner(t *testing.T) {
	e := apptest.Start(t, apptest.Options{NoOwner: true})
	c := e.Client()
	status, body := c.Do("GET", "/api/session", nil)
	if status != 200 || !strings.Contains(body, `"owner_exists":false`) {
		t.Fatalf("session: %d %s", status, body)
	}
	status, body = c.Do("POST", "/api/login", map[string]string{"name": "x", "password": "password-1"})
	if status != http.StatusConflict || !strings.Contains(body, "metagit owner create") {
		t.Fatalf("login without owner: %d %s", status, body)
	}
}

func TestMutationsNeedJSONContentType(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	c := e.LoggedIn()
	res, body := c.Raw("POST", "/api/projects", "application/x-www-form-urlencoded", "name=foo")
	if res.StatusCode != http.StatusUnsupportedMediaType || !strings.Contains(body, "application/json") {
		t.Fatalf("form post: %d %s", res.StatusCode, body)
	}
	res, _ = c.Raw("POST", "/api/projects", "text/plain", `{"name":"foo"}`)
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain post: %d", res.StatusCode)
	}
	if _, err := e.App.Store.ProjectByName(context.Background(), "foo"); err == nil {
		t.Fatal("project was created without JSON content type")
	}
}

// Scenarios "Удаление проекта" and "Неверное подтверждение" (web-console).
func TestProjectsAPI(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	c := e.LoggedIn()

	status, body := c.Do("POST", "/api/projects", map[string]string{"name": "foo"})
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	// Scenario "Создание проекта" (git-hosting): an empty repository clones.
	empty := t.TempDir() + "/empty"
	out, err := testutil.GitErr(t.TempDir(), nil, "clone", e.GitURL("foo"), empty)
	if err != nil || !strings.Contains(out, "empty repository") {
		t.Fatalf("clone of a new project: %v\n%s", err, out)
	}
	if status, body := c.Do("POST", "/api/projects", map[string]string{"name": "foo"}); status != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", status, body)
	}
	if status, body := c.Do("POST", "/api/projects", map[string]string{"name": "Bad/Name"}); status != http.StatusBadRequest || !strings.Contains(body, "a-z") {
		t.Fatalf("invalid name: %d %s", status, body)
	}
	_, body = c.Do("GET", "/api/projects", nil)
	list := apptest.JSON[[]project](t, body)
	if len(list) != 1 || list[0].HTTPURL != e.BaseURL+"/foo.git" || list[0].SSHURL != "ssh://git@"+e.SSHAddr+"/foo.git" {
		t.Fatalf("list = %+v", list)
	}

	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	tg := e.AddTarget("foo", "github", h.URL("foo"), hostToken, true)
	work := testutil.InitWork(t)
	c1 := testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")
	e.WaitSynced(tg.ID)

	status, _ = c.Do("POST", "/api/projects/foo/delete", map[string]string{"confirm": "bar"})
	if status != http.StatusBadRequest || !e.App.Repos.Exists("foo") {
		t.Fatalf("wrong confirmation: %d, repo exists %v", status, e.App.Repos.Exists("foo"))
	}
	status, _ = c.Do("POST", "/api/projects/foo/delete", map[string]string{"confirm": "foo"})
	if status != 200 || e.App.Repos.Exists("foo") {
		t.Fatalf("delete: %d", status)
	}
	if status, _ := c.Do("GET", "/api/projects/foo", nil); status != http.StatusNotFound {
		t.Fatalf("deleted project still exists: %d", status)
	}
	if h.Ref(t, "foo", "refs/heads/main") != c1 {
		t.Fatal("deleting a project must not touch the hosting")
	}
}

// Scenarios "Замена токена", "Ручная доставка" (web-console) and "Просмотр
// target'а" (mirror-replication).
func TestTargetsAPI(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	c := e.LoggedIn()
	e.CreateProject("foo")
	h := fakehost.New(t, hostToken)
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "first")
	e.Push(work, "foo", "main")

	status, body := c.Do("POST", "/api/projects/foo/targets", map[string]any{
		"name": "github", "url": h.URL("foo"), "auth_kind": "https", "username": "x", "secret": "old-token-1",
	})
	if status != http.StatusCreated || strings.Contains(body, "old-token-1") {
		t.Fatalf("create target: %d %s", status, body)
	}
	tg := apptest.JSON[target](t, body)
	if !tg.HasSecret || !tg.Enabled {
		t.Fatalf("target = %+v", tg)
	}
	path := fmt.Sprintf("/api/projects/foo/targets/%d", tg.ID)

	// Viewing never shows the secret.
	for _, p := range []string{"/api/projects/foo", "/api/projects/foo/targets", "/api/projects"} {
		_, body := c.Do("GET", p, nil)
		if strings.Contains(body, "old-token-1") {
			t.Fatalf("%s shows the secret: %s", p, body)
		}
	}
	e.WaitTarget(tg.ID, "error with old token", func(t *store.Target) bool { return t.State == store.StateError })

	// Editing without a secret keeps it, editing with a secret replaces it.
	status, body = c.Do("PUT", path, map[string]any{"name": "github", "url": h.URL("foo"), "auth_kind": "https", "username": "x", "secret": "", "enabled": false})
	if status != 200 || !apptest.JSON[target](t, body).HasSecret {
		t.Fatalf("update without secret: %d %s", status, body)
	}
	before := e.Target(tg.ID).Secret
	status, body = c.Do("PUT", path, map[string]any{"name": "github", "url": h.URL("foo"), "auth_kind": "https", "username": "x", "secret": hostToken, "enabled": true})
	if status != 200 {
		t.Fatalf("update with secret: %d %s", status, body)
	}
	if after := e.Target(tg.ID).Secret; string(after) == string(before) {
		t.Fatal("secret was not replaced")
	}
	e.WaitSynced(tg.ID)

	// Manual delivery: a commit that reaches metagit without a push hook.
	a := testutil.Commit(t, work, "a")
	testutil.Git(t, e.RepoPath("foo"), "fetch", "-q", work, "main:main")
	if status, body := c.Do("POST", path+"/sync", nil); status != http.StatusAccepted {
		t.Fatalf("sync: %d %s", status, body)
	}
	e.WaitTarget(tg.ID, "manual delivery", func(*store.Target) bool { return h.Ref(t, "foo", "refs/heads/main") == a })

	// Disabled target cannot be synced; validation errors are readable.
	c.Do("PUT", path, map[string]any{"name": "github", "url": h.URL("foo"), "auth_kind": "https", "username": "x", "enabled": false})
	if status, body := c.Do("POST", path+"/sync", nil); status != http.StatusBadRequest || !strings.Contains(body, "is disabled") {
		t.Fatalf("sync disabled: %d %s", status, body)
	}
	status, body = c.Do("POST", "/api/projects/foo/targets", map[string]any{"name": "bad", "url": "https://u:p@github.com/u/foo.git", "auth_kind": "none"})
	if status != http.StatusBadRequest || !strings.Contains(body, "credentials in the URL are not allowed") {
		t.Fatalf("secret in URL: %d %s", status, body)
	}
	status, body = c.Do("POST", "/api/projects/foo/targets", map[string]any{"name": "bad", "url": "git@host:u/foo.git", "auth_kind": "ssh", "secret": "not a key"})
	if status != http.StatusBadRequest || !strings.Contains(body, "SSH key") {
		t.Fatalf("bad ssh key: %d %s", status, body)
	}

	// Deletion of a target does not touch the hosting.
	if status, _ := c.Do("DELETE", path, nil); status != 200 {
		t.Fatalf("delete target: %d", status)
	}
	if h.Ref(t, "foo", "refs/heads/main") != a {
		t.Fatal("deleting a target must not touch the hosting")
	}
}

// Scenario "Добавление защиты" (web-console) and git credentials.
func TestProtectedTokensAndKeysAPI(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	c := e.LoggedIn()
	e.CreateProject("foo")
	work := testutil.InitWork(t)
	testutil.Commit(t, work, "a")
	e.Push(work, "foo", "main")

	status, body := c.Do("POST", "/api/projects/foo/protected", map[string]string{"pattern": "refs/heads/main"})
	if status != http.StatusCreated {
		t.Fatalf("add protected: %d %s", status, body)
	}
	if status, _ := c.Do("POST", "/api/projects/foo/protected", map[string]string{"pattern": "main"}); status != http.StatusBadRequest {
		t.Fatalf("bad pattern: %d", status)
	}
	testutil.Git(t, work, "commit", "-q", "--amend", "--allow-empty", "-m", "rewritten")
	if out, err := e.PushErr(work, "foo", "--force", "main"); err == nil || !strings.Contains(out, "is protected") {
		t.Fatalf("force-push after protection: %v\n%s", err, out)
	}
	_, body = c.Do("GET", "/api/projects/foo/protected", nil)
	prot := apptest.JSON[[]struct {
		ID      int64  `json:"id"`
		Pattern string `json:"pattern"`
	}](t, body)
	if len(prot) != 1 {
		t.Fatalf("protected = %s", body)
	}
	if status, _ := c.Do("DELETE", fmt.Sprintf("/api/projects/foo/protected/%d", prot[0].ID), nil); status != 200 {
		t.Fatal("delete protected")
	}
	e.Push(work, "foo", "--force", "main")

	// Tokens: the value is shown once.
	status, body = c.Do("POST", "/api/tokens", map[string]string{"name": "laptop"})
	created := apptest.JSON[struct {
		Value string `json:"value"`
		Token struct {
			ID int64 `json:"id"`
		} `json:"token"`
	}](t, body)
	if status != http.StatusCreated || !strings.HasPrefix(created.Value, auth.TokenPrefix) {
		t.Fatalf("create token: %d %s", status, body)
	}
	_, body = c.Do("GET", "/api/tokens", nil)
	if strings.Contains(body, created.Value) || !strings.Contains(body, "laptop") {
		t.Fatalf("token list: %s", body)
	}
	if status, _ := c.Do("DELETE", fmt.Sprintf("/api/tokens/%d", created.Token.ID), nil); status != 200 {
		t.Fatal("revoke token")
	}
	if e.App.Auth.CheckToken(context.Background(), created.Value) {
		t.Fatal("revoked token still works")
	}

	// SSH keys.
	status, body = c.Do("POST", "/api/ssh-keys", map[string]string{"name": "bad", "public_key": "ssh-rsa nope"})
	if status != http.StatusBadRequest {
		t.Fatalf("bad key: %d %s", status, body)
	}
	_, body = c.Do("GET", "/api/ssh-keys", nil)
	if !strings.Contains(body, `"fingerprint":"SHA256:`) {
		t.Fatalf("ssh keys: %s", body)
	}
}

// Scenarios "Расхождение в журнале", "Ошибка доставки в статусе" and "Архив
// после force-push" (web-console).
func TestStatusEventsAndArchiveAPI(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	c := e.LoggedIn()
	e.CreateProject("foo")
	github := fakehost.New(t, hostToken)
	github.CreateRepo(t, "foo")
	bitbucket := fakehost.New(t, hostToken)
	bitbucket.CreateRepo(t, "foo")
	gh := e.AddTarget("foo", "github", github.URL("foo"), hostToken, true)
	bb := e.AddTarget("foo", "bitbucket", bitbucket.URL("foo"), "bad-token-1", true)

	work := testutil.InitWork(t)
	testutil.Commit(t, work, "c")
	e.Push(work, "foo", "main", "main:feature")
	e.WaitSynced(gh.ID)
	e.WaitTarget(bb.ID, "error", func(t *store.Target) bool { return t.State == store.StateError })

	// Divergence on github, force-push of feature in metagit.
	other := t.TempDir() + "/other"
	testutil.Git(t, t.TempDir(), "clone", "-q", github.Path("foo"), other)
	testutil.Commit(t, other, "e on github")
	testutil.Git(t, other, "push", "-q", github.Path("foo"), "main")
	old := testutil.RefOf(t, e.RepoPath("foo"), "refs/heads/feature")
	testutil.Commit(t, work, "d")
	testutil.Git(t, work, "checkout", "-q", "--orphan", "x")
	testutil.Commit(t, work, "x")
	e.Push(work, "foo", "main", "--force", "x:feature")

	_, body := c.Do("GET", "/api/projects/foo", nil)
	details := apptest.JSON[struct {
		Targets []target `json:"targets"`
	}](t, body)
	var bbStatus target
	for _, tg := range details.Targets {
		if tg.Name == "bitbucket" {
			bbStatus = tg
		}
	}
	if bbStatus.State != "error" || !strings.Contains(bbStatus.LastError, "Authentication failed") || strings.Contains(bbStatus.LastError, "bad-token-1") {
		t.Fatalf("bitbucket status = %+v", bbStatus)
	}
	for _, tg := range details.Targets {
		if tg.Name == "github" && tg.LastSuccessAt == "" {
			t.Fatalf("github has no last success time: %+v", tg)
		}
	}

	_, body = c.Do("GET", "/api/projects/foo/events?limit=50", nil)
	events := apptest.JSON[[]struct {
		Kind       string `json:"kind"`
		TargetName string `json:"target_name"`
		Ref        string `json:"ref"`
		ArchiveRef string `json:"archive_ref"`
	}](t, body)
	var divergedOK bool
	for _, ev := range events {
		if ev.Kind == store.EventDiverged && ev.TargetName == "github" && ev.Ref == "refs/heads/main" &&
			strings.HasPrefix(ev.ArchiveRef, "refs/metagit/archive/targets/") {
			divergedOK = true
		}
	}
	if !divergedOK {
		t.Fatalf("no divergence event: %s", body)
	}

	_, body = c.Do("GET", "/api/projects/foo/archive", nil)
	archive := apptest.JSON[[]struct {
		Source       string `json:"source"`
		OrigRef      string `json:"orig_ref"`
		ArchiveRef   string `json:"archive_ref"`
		Object       string `json:"object"`
		FetchCommand string `json:"fetch_command"`
	}](t, body)
	var pushOK bool
	for _, ar := range archive {
		if ar.Source == store.SourcePush && ar.OrigRef == "refs/heads/feature" && ar.Object == old &&
			ar.FetchCommand == "git fetch "+e.BaseURL+"/foo.git "+ar.ArchiveRef {
			pushOK = true
		}
	}
	if !pushOK {
		t.Fatalf("no archive record for the force-push: %s", body)
	}
	if status, _ := c.Do("GET", "/api/projects/foo/events?limit=0", nil); status != http.StatusBadRequest {
		t.Fatal("limit=0 must be rejected")
	}
}

// Export and import through the API, as the web interface does it.
func TestExportImportAPI(t *testing.T) {
	src := apptest.Start(t, apptest.Options{})
	src.CreateProject("foo")
	c := src.LoggedIn()
	if status, body := c.Do("POST", "/api/export", map[string]string{"passphrase": "short"}); status != http.StatusBadRequest {
		t.Fatalf("short passphrase: %d %s", status, body)
	}
	res, file := c.Raw("POST", "/api/export", "application/json", `{"passphrase":"export-passphrase"}`)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/octet-stream" ||
		!strings.Contains(res.Header.Get("Content-Disposition"), `attachment; filename="metagit-settings-`) {
		t.Fatalf("export: %d %v", res.StatusCode, res.Header)
	}

	dst := apptest.Start(t, apptest.Options{})
	d := dst.LoggedIn()
	data := base64.StdEncoding.EncodeToString([]byte(file))
	status, body := d.Do("POST", "/api/import", map[string]string{"passphrase": "wrong-passphrase", "data": data})
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid password") {
		t.Fatalf("wrong passphrase: %d %s", status, body)
	}
	status, body = d.Do("POST", "/api/import", map[string]string{"passphrase": "export-passphrase", "data": data})
	if status != 200 || !strings.Contains(body, `"projects":["foo"]`) {
		t.Fatalf("import: %d %s", status, body)
	}
	if !dst.App.Repos.Exists("foo") {
		t.Fatal("project is not imported")
	}
}

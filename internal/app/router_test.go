package app_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/app"
	"github.com/ekalinin/metagit/internal/apptest"
	"github.com/ekalinin/metagit/internal/config"
)

func TestRouting(t *testing.T) {
	e := apptest.Start(t, apptest.Options{})
	e.CreateProject("foo")
	c := e.Client()

	res, body := c.Raw("GET", "/api/session", "", "")
	if res.StatusCode != 200 || !strings.Contains(body, `"owner_exists":true`) {
		t.Fatalf("/api/session: %d %s", res.StatusCode, body)
	}

	req, _ := http.NewRequest("GET", e.BaseURL+"/foo.git/info/refs?service=git-upload-pack", nil)
	req.SetBasicAuth(apptest.OwnerName, e.Token)
	gitRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	gitRes.Body.Close()
	if gitRes.StatusCode != 200 || gitRes.Header.Get("Content-Type") != "application/x-git-upload-pack-advertisement" {
		t.Fatalf("git path: %d %s", gitRes.StatusCode, gitRes.Header.Get("Content-Type"))
	}

	res, body = c.Raw("GET", "/projects/foo", "", "")
	if res.StatusCode != 200 || !strings.Contains(body, `<div id="root">`) {
		t.Fatalf("SPA route: %d %s", res.StatusCode, body)
	}
	res, _ = c.Raw("GET", "/assets/missing.js", "", "")
	if res.StatusCode != 404 {
		t.Fatalf("missing asset: %d", res.StatusCode)
	}
	res, _ = c.Raw("GET", "/api/nope", "", "")
	if res.StatusCode != 404 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unknown API path: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestStartWithoutGitFails(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := app.New(t.Context(), app.Options{Config: &config.Config{DataDir: filepath.Join(t.TempDir(), "data")}})
	if err == nil || !strings.Contains(err.Error(), "git is not found") {
		t.Fatalf("expected a clear error, got %v", err)
	}
}

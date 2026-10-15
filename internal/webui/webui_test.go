package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

func TestHandlerServesSPA(t *testing.T) {
	site := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.svg":   {Data: []byte("<svg/>")},
	}
	h := Handler(site)

	cases := []struct {
		path string
		code int
		body string
	}{
		{"/", 200, "<html>app</html>"},
		{"/projects/foo", 200, "<html>app</html>"},
		{"/assets/app.js", 200, "console.log(1)"},
		{"/favicon.svg", 200, "<svg/>"},
		{"/assets/missing.js", 404, ""},
		{"/missing.png", 404, ""},
	}
	for _, tc := range cases {
		code, body := get(t, h, tc.path)
		if code != tc.code {
			t.Errorf("%s: code %d, want %d", tc.path, code, tc.code)
		}
		if tc.body != "" && body != tc.body {
			t.Errorf("%s: body %q, want %q", tc.path, body, tc.body)
		}
	}
}

func TestHandlerWithoutBuild(t *testing.T) {
	code, body := get(t, Handler(nil), "/")
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "make build") {
		t.Fatalf("code %d, body %q", code, body)
	}
}

func TestEmbeddedBuild(t *testing.T) {
	site := Embedded()
	if site == nil {
		t.Skip("web interface is not built; run make build")
	}
	code, body := get(t, Handler(site), "/")
	if code != 200 || !strings.Contains(body, `<div id="root">`) {
		t.Fatalf("code %d, body %q", code, body)
	}
}

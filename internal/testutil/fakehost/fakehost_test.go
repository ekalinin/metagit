package fakehost

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ekalinin/metagit/internal/testutil"
)

func TestPushToFakeHost(t *testing.T) {
	h := New(t, "host-token")
	h.CreateRepo(t, "foo")
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "first")

	u, _ := url.Parse(h.URL("foo"))
	u.User = url.UserPassword("x", "host-token")
	if out, err := testutil.GitErr(work, GitEnv, "push", "-q", u.String(), "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if got := h.Ref(t, "foo", "refs/heads/main"); got != c {
		t.Fatalf("host main = %q", got)
	}

	u.User = url.UserPassword("x", "wrong")
	if out, err := testutil.GitErr(work, GitEnv, "push", u.String(), "main"); err == nil || !strings.Contains(out, "Authentication failed") {
		t.Fatalf("wrong token: %v\n%s", err, out)
	}

	h.SetPublic(true)
	if out, err := testutil.GitErr(work, GitEnv, "ls-remote", h.URL("foo")); err != nil || !strings.Contains(out, c) {
		t.Fatalf("public host, ls-remote without a token: %v\n%s", err, out)
	}
	if out, err := testutil.GitErr(work, GitEnv, "push", u.String(), "main:refs/heads/other"); err == nil || !strings.Contains(out, "Authentication failed") {
		t.Fatalf("public host, push with a wrong token: %v\n%s", err, out)
	}
	h.SetPublic(false)

	h.SetDown(true)
	u.User = url.UserPassword("x", "host-token")
	if _, err := testutil.GitErr(work, GitEnv, "ls-remote", u.String()); err == nil {
		t.Fatal("host is down, ls-remote must fail")
	}
	if h.Requests() == 0 {
		t.Fatal("requests are not counted")
	}
}

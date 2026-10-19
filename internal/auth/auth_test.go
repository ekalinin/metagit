package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/store"
)

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "metagit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("hash = %q", h)
	}
	if !VerifyPassword(h, "correct horse") {
		t.Fatal("right password rejected")
	}
	if VerifyPassword(h, "wrong horse") || VerifyPassword("garbage", "correct horse") {
		t.Fatal("wrong password accepted")
	}
}

func TestCreateOwner(t *testing.T) {
	a := newService(t)
	ctx := t.Context()
	if _, err := a.CreateOwner(ctx, "eugene", "short"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short password: %v", err)
	}
	created, err := a.CreateOwner(ctx, "eugene", "password-1")
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	created, err = a.CreateOwner(ctx, "eugene", "password-2")
	if err != nil || created {
		t.Fatalf("change password: %v %v", created, err)
	}
	if _, err := a.Login(ctx, "eugene", "password-1", "10.0.0.1:1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password must stop working: %v", err)
	}
	if _, err := a.Login(ctx, "eugene", "password-2", "10.0.0.1:1"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if _, err := a.CreateOwner(ctx, "someone", "password-3"); !errors.Is(err, ErrOwnerExists) {
		t.Fatalf("second owner must be refused: %v", err)
	}
}

func TestLoginSessionLogout(t *testing.T) {
	a := newService(t)
	ctx := t.Context()
	if _, err := a.Login(ctx, "eugene", "password-1", "1.1.1.1:1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login without owner: %v", err)
	}
	if _, err := a.CreateOwner(ctx, "eugene", "password-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login(ctx, "eugene", "nope-nope", "1.1.1.1:1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := a.Login(ctx, "other", "password-1", "1.1.1.1:1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong name: %v", err)
	}
	id, err := a.Login(ctx, "eugene", "password-1", "1.1.1.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if !a.ValidSession(ctx, id) {
		t.Fatal("new session must be valid")
	}
	if a.ValidSession(ctx, "forged") || a.ValidSession(ctx, "") {
		t.Fatal("unknown session accepted")
	}
	if err := a.Logout(ctx, id); err != nil {
		t.Fatal(err)
	}
	if a.ValidSession(ctx, id) {
		t.Fatal("session must end after logout")
	}
}

func TestSessionExpires(t *testing.T) {
	a := newService(t)
	ctx := t.Context()
	if _, err := a.CreateOwner(ctx, "eugene", "password-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a.now = func() time.Time { return now }
	id, err := a.Login(ctx, "eugene", "password-1", "1.1.1.1:1")
	if err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return now.Add(a.sessionTTL + time.Minute) }
	if a.ValidSession(ctx, id) {
		t.Fatal("expired session accepted")
	}
}

func TestLoginRateLimit(t *testing.T) {
	a := newService(t)
	ctx := t.Context()
	if _, err := a.CreateOwner(ctx, "eugene", "password-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a.now = func() time.Time { return now }
	for range 5 {
		if _, err := a.Login(ctx, "eugene", "wrong-pass", "2.2.2.2:1000"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	if _, err := a.Login(ctx, "eugene", "password-1", "2.2.2.2:2000"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if _, err := a.Login(ctx, "eugene", "password-1", "3.3.3.3:1"); err != nil {
		t.Fatalf("other client must not be blocked: %v", err)
	}
	a.now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, err := a.Login(ctx, "eugene", "password-1", "2.2.2.2:1000"); err != nil {
		t.Fatalf("block must end after the window: %v", err)
	}
}

func TestClientAddr(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("::1/128")}
	cases := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"no trusted proxies", "10.0.0.1:1000", []string{"198.51.100.1"}, nil, "10.0.0.1:1000"},
		{"untrusted peer", "203.0.113.9:1000", []string{"198.51.100.1"}, trusted, "203.0.113.9:1000"},
		{"no header", "10.0.0.1:1000", nil, trusted, "10.0.0.1:1000"},
		{"client of the proxy", "10.0.0.1:1000", []string{"198.51.100.1"}, trusted, "198.51.100.1"},
		{"spoofed left part", "10.0.0.1:1000", []string{"1.1.1.1, 198.51.100.1"}, trusted, "198.51.100.1"},
		{"chain of proxies", "10.0.0.1:1000", []string{"1.1.1.1, 198.51.100.1, 192.168.1.5"}, trusted, "198.51.100.1"},
		{"several headers", "10.0.0.1:1000", []string{"1.1.1.1", "198.51.100.1, 192.168.1.5"}, trusted, "198.51.100.1"},
		{"garbage left of the client", "10.0.0.1:1000", []string{"garbage, 198.51.100.1"}, trusted, "198.51.100.1"},
		{"garbage", "10.0.0.1:1000", []string{"garbage"}, trusted, "10.0.0.1:1000"},
		{"empty header", "10.0.0.1:1000", []string{""}, trusted, "10.0.0.1:1000"},
		{"only proxies", "10.0.0.1:1000", []string{"192.168.1.5"}, trusted, "10.0.0.1:1000"},
		{"IPv6", "[::1]:1000", []string{"2001:db8::1"}, trusted, "2001:db8::1"},
		{"IPv4-mapped", "10.0.0.1:1000", []string{"::ffff:198.51.100.1"}, trusted, "198.51.100.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := ClientAddr(r, tc.trusted); got != tc.want {
				t.Fatalf("ClientAddr = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSessionCookieFlags(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "abc", time.Hour, true)
	c := rec.Result().Cookies()[0]
	if c.Name != SessionCookie || c.Value != "abc" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("cookie = %+v", c)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	if SessionFromRequest(req) != "abc" {
		t.Fatal("cookie not read back")
	}
	rec = httptest.NewRecorder()
	ClearSessionCookie(rec, false)
	if c := rec.Result().Cookies()[0]; c.MaxAge >= 0 || c.Secure {
		t.Fatalf("clear cookie = %+v", c)
	}
}

func TestTokens(t *testing.T) {
	a := newService(t)
	ctx := t.Context()
	value, tok, err := a.CreateToken(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(value, TokenPrefix) || len(value) < 40 {
		t.Fatalf("token value = %q", value)
	}
	list, _ := a.ListTokens(ctx)
	if len(list) != 1 || list[0].Name != "laptop" || list[0].Hash == value || strings.Contains(list[0].Hash, value) {
		t.Fatalf("token list must not expose the value: %+v", list)
	}
	if !a.CheckToken(ctx, value) {
		t.Fatal("token rejected")
	}
	if list, _ := a.ListTokens(ctx); list[0].LastUsedAt.IsZero() {
		t.Fatal("last use is not recorded")
	}
	if a.CheckToken(ctx, value+"x") || a.CheckToken(ctx, "not-a-token") {
		t.Fatal("wrong token accepted")
	}
	if err := a.RevokeToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if a.CheckToken(ctx, value) {
		t.Fatal("revoked token accepted")
	}
	if _, _, err := a.CreateToken(ctx, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name: %v", err)
	}
}

func TestSSHKeys(t *testing.T) {
	a := newService(t)
	ctx := t.Context()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " eugene@laptop"

	if _, err := a.AddSSHKey(ctx, "", "ssh-ed25519 garbage"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad key: %v", err)
	}
	k, err := a.AddSSHKey(ctx, "", line)
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "eugene@laptop" || k.Fingerprint != ssh.FingerprintSHA256(sshPub) {
		t.Fatalf("key = %+v", k)
	}
	if _, err := a.AddSSHKey(ctx, "again", line); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate key: %v", err)
	}
	if !a.CheckSSHKey(ctx, sshPub) {
		t.Fatal("registered key rejected")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	otherSSH, _ := ssh.NewPublicKey(otherPub)
	if a.CheckSSHKey(ctx, otherSSH) {
		t.Fatal("unknown key accepted")
	}
	if err := a.DeleteSSHKey(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if a.CheckSSHKey(ctx, sshPub) {
		t.Fatal("deleted key accepted")
	}
}

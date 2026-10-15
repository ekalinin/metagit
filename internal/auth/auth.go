package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/store"
)

// Errors returned by Service.
var (
	ErrOwnerExists        = errors.New("owner already exists: metagit has exactly one owner")
	ErrInvalidCredentials = errors.New("invalid name or password")
	ErrTooManyAttempts    = errors.New("too many failed login attempts, try again later")
	ErrInvalidInput       = errors.New("invalid input")
)

// MinPasswordLength is the minimal length of the owner's password.
const MinPasswordLength = 8

// TokenPrefix starts every git access token.
const TokenPrefix = "mgt_"

// SessionCookie is the name of the web session cookie.
const SessionCookie = "metagit_session"

// Service implements owner, session, token and SSH key operations.
type Service struct {
	store      *store.Store
	now        func() time.Time
	sessionTTL time.Duration
	limiter    *limiter
}

// New creates a Service.
func New(s *store.Store) *Service {
	return &Service{
		store:      s,
		now:        time.Now,
		sessionTTL: 7 * 24 * time.Hour,
		limiter:    newLimiter(5, 15*time.Minute),
	}
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// CreateOwner creates the owner, or changes the password when name matches
// the existing owner. It reports whether a new owner was created.
func (a *Service) CreateOwner(ctx context.Context, name, password string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("%w: name must not be empty", ErrInvalidInput)
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return false, fmt.Errorf("%w: password must have at least %d characters", ErrInvalidInput, MinPasswordLength)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	owner, err := a.store.GetOwner(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if err := a.store.CreateOwner(ctx, name, hash); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return false, ErrOwnerExists
			}
			return false, err
		}
		return true, nil
	case err != nil:
		return false, err
	case owner.Name != name:
		return false, fmt.Errorf("%w (owner name is %q)", ErrOwnerExists, owner.Name)
	default:
		return false, a.store.UpdateOwnerPassword(ctx, hash)
	}
}

// Owner returns the owner name and whether the owner exists.
func (a *Service) Owner(ctx context.Context) (string, bool, error) {
	owner, err := a.store.GetOwner(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return owner.Name, true, nil
}

// Login checks the credentials and returns a new session identifier.
// remoteAddr is used to limit the rate of failed attempts.
func (a *Service) Login(ctx context.Context, name, password, remoteAddr string) (string, error) {
	client := clientKey(remoteAddr)
	now := a.now()
	if a.limiter.blocked(client, now) {
		return "", ErrTooManyAttempts
	}
	owner, err := a.store.GetOwner(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	ok := owner != nil &&
		subtle.ConstantTimeCompare([]byte(owner.Name), []byte(name)) == 1 &&
		VerifyPassword(owner.PasswordHash, password)
	if !ok {
		a.limiter.fail(client, now)
		return "", ErrInvalidCredentials
	}
	a.limiter.reset(client)
	id := randomString(32)
	if err := a.store.CreateSession(ctx, hashHex(id), now.Add(a.sessionTTL)); err != nil {
		return "", err
	}
	_ = a.store.DeleteExpiredSessions(ctx, now)
	return id, nil
}

// ValidSession reports whether the session identifier is active.
func (a *Service) ValidSession(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	ok, err := a.store.SessionValid(ctx, hashHex(id), a.now())
	return err == nil && ok
}

// Logout ends a session.
func (a *Service) Logout(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return a.store.DeleteSession(ctx, hashHex(id))
}

// SessionTTL returns the lifetime of a session.
func (a *Service) SessionTTL() time.Duration { return a.sessionTTL }

// SetSessionCookie writes the session cookie.
func SetSessionCookie(w http.ResponseWriter, id string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// SessionFromRequest returns the session identifier from the cookie.
func SessionFromRequest(r *http.Request) string {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// CreateToken creates a git access token and returns its value. The value is
// not stored and cannot be shown again.
func (a *Service) CreateToken(ctx context.Context, name string) (string, store.AccessToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", store.AccessToken{}, fmt.Errorf("%w: token name must not be empty", ErrInvalidInput)
	}
	value := TokenPrefix + randomString(32)
	t, err := a.store.CreateToken(ctx, name, hashHex(value))
	return value, t, err
}

// ListTokens returns tokens without their values.
func (a *Service) ListTokens(ctx context.Context) ([]store.AccessToken, error) {
	return a.store.ListTokens(ctx)
}

// RevokeToken deletes a token.
func (a *Service) RevokeToken(ctx context.Context, id int64) error {
	return a.store.DeleteToken(ctx, id)
}

// CheckToken reports whether value is an active token.
func (a *Service) CheckToken(ctx context.Context, value string) bool {
	if !strings.HasPrefix(value, TokenPrefix) {
		return false
	}
	t, err := a.store.TokenByHash(ctx, hashHex(value))
	if err != nil {
		return false
	}
	_ = a.store.TouchToken(ctx, t.ID, a.now())
	return true
}

// AddSSHKey stores a public key given in authorized_keys format.
func (a *Service) AddSSHKey(ctx context.Context, name, authorizedKey string) (store.SSHKey, error) {
	key, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(authorizedKey)))
	if err != nil {
		return store.SSHKey{}, fmt.Errorf("%w: cannot parse SSH public key: %v", ErrInvalidInput, err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = comment
	}
	if name == "" {
		name = key.Type()
	}
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	return a.store.CreateSSHKey(ctx, name, normalized, ssh.FingerprintSHA256(key))
}

// ListSSHKeys returns the owner's keys.
func (a *Service) ListSSHKeys(ctx context.Context) ([]store.SSHKey, error) {
	return a.store.ListSSHKeys(ctx)
}

// DeleteSSHKey removes a key.
func (a *Service) DeleteSSHKey(ctx context.Context, id int64) error {
	return a.store.DeleteSSHKey(ctx, id)
}

// CheckSSHKey reports whether the key belongs to the owner.
func (a *Service) CheckSSHKey(ctx context.Context, key ssh.PublicKey) bool {
	stored, err := a.store.SSHKeyByFingerprint(ctx, ssh.FingerprintSHA256(key))
	if err != nil {
		return false
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(stored.PublicKey))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(parsed.Marshal(), key.Marshal()) == 1
}

func clientKey(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// limiter blocks a client after too many failures within a window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, fails: map[string][]time.Time{}}
}

func (l *limiter) recent(client string, now time.Time) []time.Time {
	var kept []time.Time
	for _, t := range l.fails[client] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, client)
	} else {
		l.fails[client] = kept
	}
	return kept
}

func (l *limiter) blocked(client string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(client, now)) >= l.max
}

func (l *limiter) fail(client string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[client] = append(l.recent(client, now), now)
}

func (l *limiter) reset(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, client)
}

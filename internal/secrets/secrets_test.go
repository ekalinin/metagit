package secrets

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key, err := LoadKey(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	box, err := NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("ghp_supersecret")
	sealed, err := box.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("ciphertext contains the plain text")
	}
	again, _ := box.Seal(plain)
	if bytes.Equal(sealed, again) {
		t.Fatal("two encryptions must differ (random nonce)")
	}
	got, err := box.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %q %v", got, err)
	}
	if s, err := box.Seal(nil); s != nil || err != nil {
		t.Fatalf("empty secret: %v %v", s, err)
	}
}

func TestOpenWithOtherKeyFails(t *testing.T) {
	k1, _ := LoadKey(t.TempDir(), "")
	k2, _ := LoadKey(t.TempDir(), "")
	b1, _ := NewBox(k1)
	b2, _ := NewBox(k2)
	sealed, _ := b1.Seal([]byte("token"))
	if _, err := b2.Open(sealed); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt, got %v", err)
	}
	if _, err := b1.Open([]byte("short")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt for damaged data, got %v", err)
	}
}

func TestKeyFileCreatedWithMode0600AndReused(t *testing.T) {
	dir := t.TempDir()
	k1, err := LoadKey(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v", info.Mode().Perm())
	}
	k2, err := LoadKey(dir, "")
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("key must be reused: %v", err)
	}
}

func TestKeyFileWithOpenPermissionsRejected(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadKey(dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, KeyFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(dir, ""); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestKeyFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	hexKey := strings.Repeat("ab", KeySize)
	key, err := LoadKey(dir, hexKey)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(hexKey)
	if !bytes.Equal(key, want) {
		t.Fatal("hex key decoded wrongly")
	}
	if _, err := os.Stat(filepath.Join(dir, KeyFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("key file must not be created when the key comes from the environment")
	}
	if _, err := LoadKey(dir, GenerateKey()); err != nil {
		t.Fatalf("base64 key: %v", err)
	}
	if _, err := LoadKey(dir, "short"); err == nil {
		t.Fatal("expected error for a bad key")
	}
}

func TestMask(t *testing.T) {
	cases := []struct {
		in      string
		secrets []string
		want    string
	}{
		{
			"remote: Invalid username or token ghp_abc123 for 'https://github.com/u/foo.git'",
			[]string{"ghp_abc123"},
			"remote: Invalid username or token *** for 'https://github.com/u/foo.git'",
		},
		{
			"fatal: unable to access 'https://user:pass@example.com/x.git/': 403",
			nil,
			"fatal: unable to access 'https://***@example.com/x.git/': 403",
		},
		{
			"fatal: could not read from ssh://git@host:22/x.git",
			nil,
			"fatal: could not read from ssh://***@host:22/x.git",
		},
		{"nothing to hide", []string{"", "ab"}, "nothing to hide"},
	}
	for _, tc := range cases {
		if got := Mask(tc.in, tc.secrets...); got != tc.want {
			t.Errorf("Mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Package secrets encrypts target credentials and masks secrets in text.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// KeySize is the size of the master key (AES-256).
const KeySize = 32

// KeyFile is the name of the master key file in the data directory.
const KeyFile = "secret.key"

// LoadKey returns the master key. A non-empty envValue (base64 or hex of 32
// bytes, normally METAGIT_SECRET_KEY) wins. Otherwise the key is read from
// <dataDir>/secret.key, which is created with mode 0600 when missing.
func LoadKey(dataDir, envValue string) ([]byte, error) {
	if envValue != "" {
		key, err := decodeKey(envValue)
		if err != nil {
			return nil, fmt.Errorf("METAGIT_SECRET_KEY: %w", err)
		}
		return key, nil
	}
	path := filepath.Join(dataDir, KeyFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return createKeyFile(path)
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is accessible by other users (mode %v): run chmod 600 %s", path, info.Mode().Perm(), path)
	}
	key, err := decodeKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return key, nil
}

// GenerateKey returns a new random master key encoded in base64.
func GenerateKey() string {
	key := make([]byte, KeySize)
	_, _ = rand.Read(key)
	return base64.StdEncoding.EncodeToString(key)
}

func createKeyFile(path string) ([]byte, error) {
	encoded := GenerateKey()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(encoded + "\n"); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return decodeKey(encoded)
}

func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if key, err := base64.StdEncoding.DecodeString(s); err == nil && len(key) == KeySize {
		return key, nil
	}
	if key, err := hex.DecodeString(s); err == nil && len(key) == KeySize {
		return key, nil
	}
	return nil, fmt.Errorf("key must be %d bytes encoded in base64 or hex", KeySize)
}

// Box encrypts and decrypts with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// NewBox creates a Box with the master key.
func NewBox(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// ErrDecrypt is returned when data cannot be decrypted with the key.
var ErrDecrypt = errors.New("cannot decrypt secret: wrong key or damaged data")

// Seal encrypts plain. Empty input gives nil, so "no secret" stays empty.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts data produced by Seal.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrDecrypt
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

// MaskString replaces secrets in text.
const MaskString = "***"

var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/@\s'"]+@`)

// Mask removes the given secrets and the userinfo of URLs from text.
func Mask(text string, secrets ...string) string {
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if len(s) < 4 {
			// Too short values would mask ordinary words.
			continue
		}
		text = strings.ReplaceAll(text, s, MaskString)
	}
	return urlUserinfo.ReplaceAllString(text, "${1}"+MaskString+"@")
}

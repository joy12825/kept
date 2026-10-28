// Package secret encrypts the few secrets kept in the database that the
// server must read back (v1.7.2: storage keys entered on the page). The
// master key is KEPT_SECRET_KEY, or a key file made in the data directory
// on first use; backups leave it out, so a stolen backup does not carry
// the keys it protects.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the key file in the data directory.
const FileName = "secret.key"

// ErrDecrypt: the secret was sealed with another master key, or damaged.
var ErrDecrypt = errors.New("解不开：主密钥变了（secret.key 或 KEPT_SECRET_KEY），或者数据损坏")

// Box seals and opens secrets with the master key.
type Box struct{ aead cipher.AEAD }

// New makes a box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("the master key must be 32 bytes, got %d", len(key))
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

// ParseKey reads a key written as base64 (KEPT_SECRET_KEY, the key file).
func ParseKey(s string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(k) != 32 {
		return nil, errors.New("must be 32 bytes in base64, e.g. from `openssl rand -base64 32`")
	}
	return k, nil
}

// Load is the box of the instance: envKey when given, else the key file
// in dataDir, made when missing.
func Load(dataDir, envKey string) (*Box, error) {
	if envKey != "" {
		k, err := ParseKey(envKey)
		if err != nil {
			return nil, fmt.Errorf("KEPT_SECRET_KEY: %w", err)
		}
		return New(k)
	}
	p := filepath.Join(dataDir, FileName)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) { // made meanwhile by another process
			return Load(dataDir, "")
		}
		if err != nil {
			return nil, err
		}
		_, werr := f.WriteString(base64.StdEncoding.EncodeToString(k) + "\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(p)
			return nil, werr
		}
		return New(k)
	}
	if err != nil {
		return nil, err
	}
	k, err := ParseKey(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return New(k)
}

// Seal encrypts plain; "" stays "".
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Open decrypts what Seal made.
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if !strings.HasPrefix(sealed, "v1:") || err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	n := b.aead.NonceSize()
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

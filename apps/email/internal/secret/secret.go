// Package secret seals mailbox passwords with AES-GCM.
// Ciphertext is base64(nonce|ciphertext). Plaintext never belongs in logs.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

// ErrKey is a missing or wrong-length secrets key.
var ErrKey = errors.New("secrets key")

// Encrypt seals plaintext. The result is base64(nonce|ciphertext).
func Encrypt(key []byte, plain string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrKey
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

// Decrypt opens a value from Encrypt. A wrong key returns an error.
func Decrypt(key []byte, blob string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrKey
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Scrub removes a password from an error string before it is stored or shown.
func Scrub(err error, password string) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if password != "" {
		s = strings.ReplaceAll(s, password, "[redacted]")
	}
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}

package suite

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// ErrKey is a missing or wrong-length apps key.
var ErrKey = errors.New("apps key")

// Encrypt seals plaintext with AES-GCM. The result is base64(nonce|ciphertext).
func Encrypt(key, plain []byte) (string, error) {
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
	out := gcm.Seal(nonce, nonce, plain, nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

// Decrypt opens a value from Encrypt. A wrong key returns an error.
func Decrypt(key []byte, blob string) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(blob)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrKey
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

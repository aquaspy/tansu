package secret

import (
	"errors"
	"strings"
	"testing"
)

func TestEncryptRoundTrip(t *testing.T) {
	key := bytes32()
	blob, err := Encrypt(key, "app-password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, "app-password") {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := Decrypt(key, blob)
	if err != nil || got != "app-password" {
		t.Fatalf("decrypt %q %v", got, err)
	}
	other := bytes32()
	other[0] ^= 0xff
	if _, err := Decrypt(other, blob); err == nil {
		t.Fatal("wrong key opened the blob")
	}
}

func TestScrub(t *testing.T) {
	err := errors.New("auth failed for secret-pass")
	got := Scrub(err, "secret-pass")
	if strings.Contains(got, "secret-pass") {
		t.Fatal(got)
	}
}

func bytes32() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}

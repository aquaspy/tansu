package suite

import "testing"

func TestSameAccount(t *testing.T) {
	cases := []struct {
		aSub, aMail, bSub, bMail string
		want                     bool
	}{
		{"s1", "a@x", "s1", "b@x", true},
		{"s1", "a@x", "s2", "a@x", false},
		{"s1", "A@x", "", "a@x", true},
		{"", "a@x", "s2", "a@x", true},
		{"", "a@x", "", "b@x", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		if got := SameAccount(c.aSub, c.aMail, c.bSub, c.bMail); got != c.want {
			t.Errorf("SameAccount(%q,%q,%q,%q)=%v", c.aSub, c.aMail, c.bSub, c.bMail, got)
		}
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	blob, err := Encrypt(key, []byte("kura_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if blob == "kura_secret" || len(blob) < 20 {
		t.Fatalf("blob %q", blob)
	}
	out, err := Decrypt(key, blob)
	if err != nil || string(out) != "kura_secret" {
		t.Fatalf("decrypt %q %v", out, err)
	}
	key[0] ^= 0xff
	if _, err := Decrypt(key, blob); err == nil {
		t.Fatal("wrong key opened the token")
	}
}

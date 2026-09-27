package store

import (
	"errors"
	"testing"
	"time"
)

func TestPasswordResetReplaceConsumeAndSessions(t *testing.T) {
	st := openTest(t)
	seedClient(t, st)
	user := seedUser(t, st, "Ada@Example.com")
	if _, err := st.CreateSession(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCode("kurapeople", user.ID, "http://127.0.0.1:3005/login/kura/callback", "challenge"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccessToken("kurapeople", user.ID); err != nil {
		t.Fatal(err)
	}

	first, _, err := st.SavePasswordReset(user.ID, "pt-BR")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := st.SavePasswordReset(user.ID, "nope")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPasswordResetByToken(first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token: %v", err)
	}
	got, err := st.FindPasswordResetByToken(second)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != user.ID || got.Locale != "en" {
		t.Fatalf("reset: %+v", got)
	}
	if got.ExpiresAt.Before(time.Now().Add(PasswordResetTTL-time.Minute)) || got.ExpiresAt.After(time.Now().Add(PasswordResetTTL+time.Minute)) {
		t.Fatalf("ttl: %s", got.ExpiresAt)
	}
	if err := st.ConsumePasswordReset(second, "new-digest"); err != nil {
		t.Fatal(err)
	}
	if err := st.ConsumePasswordReset(second, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second consume: %v", err)
	}
	updated, err := st.FindUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordDigest != "new-digest" {
		t.Fatalf("digest: %s", updated.PasswordDigest)
	}
	var sessions, codes, tokens, resets int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM auth_codes WHERE user_id = ?`, user.ID).Scan(&codes); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM access_tokens WHERE user_id = ?`, user.ID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&resets); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || codes != 0 || tokens != 0 || resets != 0 {
		t.Fatalf("left sessions=%d codes=%d tokens=%d resets=%d", sessions, codes, tokens, resets)
	}
}

func TestPasswordResetExpired(t *testing.T) {
	st := openTest(t)
	user := seedUser(t, st, "ada@example.com")
	token, _, err := st.SavePasswordReset(user.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE password_resets SET expires_at = ?`, "1999-01-01 00:00:00.000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPasswordResetByToken(token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find expired: %v", err)
	}
	if err := st.ConsumePasswordReset(token, "new-digest"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consume expired: %v", err)
	}
	got, err := st.FindUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordDigest != "digest" {
		t.Fatalf("digest changed: %s", got.PasswordDigest)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expired row left behind: %d", n)
	}
}

func TestRollbackPasswordResetRestoresPreviousLink(t *testing.T) {
	st := openTest(t)
	user := seedUser(t, st, "ada@example.com")
	first, _, err := st.SavePasswordReset(user.ID, "pt")
	if err != nil {
		t.Fatal(err)
	}
	second, rb, err := st.SavePasswordReset(user.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RollbackPasswordReset(rb); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPasswordResetByToken(second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back token still live: %v", err)
	}
	got, err := st.FindPasswordResetByToken(first)
	if err != nil {
		t.Fatal(err)
	}
	if got.Locale != "pt" {
		t.Fatalf("restored: %+v", got)
	}
}

func TestDeleteStaleOAuthDropsExpiredPasswordReset(t *testing.T) {
	st := openTest(t)
	user := seedUser(t, st, "ada@example.com")
	if _, _, err := st.SavePasswordReset(user.ID, "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE password_resets SET expires_at = ?`, "1999-01-01 00:00:00.000000"); err != nil {
		t.Fatal(err)
	}
	st.DeleteStaleOAuth()
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sweep left %d rows", n)
	}
}

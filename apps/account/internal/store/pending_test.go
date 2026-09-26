package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestPendingSignupReplaceAndConsume(t *testing.T) {
	st := openTest(t)
	first, _, err := st.SavePendingSignup("Ada@Example.com", "digest-1", "/authorize?client_id=kurapeople", "pt")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := st.SavePendingSignup("ada@example.com", "digest-2", "", "nope")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPendingByToken(first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token: %v", err)
	}
	got, err := st.FindPendingByToken(second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "ada@example.com" || got.PasswordDigest != "digest-2" || got.Locale != "en" || got.Next != "" {
		t.Fatalf("pending: %+v", got)
	}
	user, err := st.ConsumePendingSignup(second)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "ada@example.com" || user.PasswordDigest != "digest-2" || user.ID == 0 {
		t.Fatalf("user: %+v", user)
	}
	if _, err := st.ConsumePendingSignup(second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second consume: %v", err)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM pending_signups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("pending rows = %d", n)
	}
}

func TestPendingSignupExpired(t *testing.T) {
	st := openTest(t)
	token, _, err := st.SavePendingSignup("ada@example.com", "digest", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE pending_signups SET expires_at = ?`, "1999-01-01 00:00:00.000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPendingByToken(token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find expired: %v", err)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM pending_signups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expired row left behind: %d", n)
	}
}

func TestDeleteStaleOAuthDropsExpiredPending(t *testing.T) {
	st := openTest(t)
	if _, _, err := st.SavePendingSignup("ada@example.com", "digest", "", "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE pending_signups SET expires_at = ?`, "1999-01-01 00:00:00.000000"); err != nil {
		t.Fatal(err)
	}
	st.DeleteStaleOAuth()
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM pending_signups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sweep left %d rows", n)
	}
}

func TestConsumePendingRejectsExistingUser(t *testing.T) {
	st := openTest(t)
	seedUser(t, st, "ada@example.com")
	token, _, err := st.SavePendingSignup("ada@example.com", "other-digest", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumePendingSignup(token); !IsUniqueViolation(err) {
		t.Fatalf("consume: %v", err)
	}
	got, err := st.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordDigest != "digest" {
		t.Fatalf("existing digest changed: %s", got.PasswordDigest)
	}
}

func TestRollbackPendingRestoresPreviousLink(t *testing.T) {
	st := openTest(t)
	first, _, err := st.SavePendingSignup("ada@example.com", "digest-1", "/authorize?a=1", "pt")
	if err != nil {
		t.Fatal(err)
	}
	second, rb, err := st.SavePendingSignup("ada@example.com", "digest-2", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RollbackPending(rb); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPendingByToken(second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back token still live: %v", err)
	}
	got, err := st.FindPendingByToken(first)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordDigest != "digest-1" || got.Locale != "pt" || got.Next != "/authorize?a=1" {
		t.Fatalf("restored: %+v", got)
	}
}

func TestRollbackPendingLeavesNewerSignup(t *testing.T) {
	st := openTest(t)
	first, rb1, err := st.SavePendingSignup("ada@example.com", "digest-1", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := st.SavePendingSignup("ada@example.com", "digest-2", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RollbackPending(rb1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPendingByToken(first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token: %v", err)
	}
	got, err := st.FindPendingByToken(second)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordDigest != "digest-2" {
		t.Fatalf("newer signup lost: %+v", got)
	}
}

func TestConfirmFailuresBurnThePendingSignup(t *testing.T) {
	st := openTest(t)
	token, _, err := st.SavePendingSignup("ada@example.com", "digest", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < ConfirmAttemptLimit; i++ {
		burned, err := st.RecordConfirmFailure(token)
		if err != nil || burned {
			t.Fatalf("attempt %d: burned=%v err=%v", i, burned, err)
		}
	}
	burned, err := st.RecordConfirmFailure(token)
	if err != nil || !burned {
		t.Fatalf("limit: burned=%v err=%v", burned, err)
	}
	if _, err := st.FindPendingByToken(token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("burned token: %v", err)
	}
}

func TestMigratePendingAttempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE pending_signups (
		email TEXT PRIMARY KEY,
		password_digest TEXT NOT NULL,
		token_digest TEXT NOT NULL UNIQUE,
		next TEXT NOT NULL DEFAULT '',
		locale TEXT NOT NULL DEFAULT 'en',
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO pending_signups
		(email, password_digest, token_digest, next, locale, expires_at, created_at)
		VALUES ('ada@example.com', 'digest', 'abc', '', 'en', '2099-01-01 00:00:00.000000', '2099-01-01 00:00:00.000000')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var attempts int
	if err := st.DB().QueryRow(`SELECT attempts FROM pending_signups WHERE email = ?`, "ada@example.com").Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d", attempts)
	}
}

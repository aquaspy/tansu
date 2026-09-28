package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeTimezone(t *testing.T) {
	ok := map[string]string{
		"America/Sao_Paulo": "America/Sao_Paulo",
		"UTC":               "UTC",
		"Etc/UTC":           "UTC",
		"  Etc/UTC  ":       "UTC",
		"":                  "UTC",
		"   ":               "UTC",
	}
	for raw, want := range ok {
		got, err := NormalizeTimezone(raw)
		if err != nil || got != want {
			t.Fatalf("NormalizeTimezone(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	bad := []string{"BRT", "EST", "-03:00", "+00:00", "Local", "local", strings.Repeat("A", 65), "Not/AZone"}
	for _, raw := range bad {
		if _, err := NormalizeTimezone(raw); err == nil {
			t.Fatalf("NormalizeTimezone(%q) accepted", raw)
		}
	}
	if _, err := NormalizeTimezone(strings.Repeat("A", 64)); err == nil {
		t.Fatal("64-byte garbage accepted")
	}
}

func TestMigrateUsersTimezone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		password_digest TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO users (email, password_digest, created_at, updated_at)
		VALUES ('ada@example.com', 'digest', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`)
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
	u, err := st.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.Timezone != "UTC" {
		t.Fatalf("existing timezone = %q", u.Timezone)
	}
	if err := st.UpdateUserTimezone(u.ID, "America/Sao_Paulo"); err != nil {
		t.Fatal(err)
	}
	u, err = st.FindUser(u.ID)
	if err != nil || u.Timezone != "America/Sao_Paulo" {
		t.Fatalf("updated = %+v err=%v", u, err)
	}
	created, err := st.CreateUser("new@example.com", "digest")
	if err != nil || created.Timezone != "UTC" {
		t.Fatalf("new user = %+v err=%v", created, err)
	}
}

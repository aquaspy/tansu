// Package store owns SQLite access: schema, pragmas, and typed queries.
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const timeLayout = "2006-01-02 15:04:05.999999"

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  password_digest TEXT NOT NULL,
  account_sub TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS kura_logins (
  state TEXT PRIMARY KEY,
  verifier TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS mailboxes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  display_name TEXT NOT NULL DEFAULT '',
  from_address TEXT NOT NULL,
  username TEXT NOT NULL,
  password_enc TEXT NOT NULL,
  imap_host TEXT NOT NULL,
  imap_port INTEGER NOT NULL,
  imap_tls TEXT NOT NULL DEFAULT 'tls',
  smtp_host TEXT NOT NULL,
  smtp_port INTEGER NOT NULL,
  smtp_tls TEXT NOT NULL DEFAULT 'tls',
  last_ok TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_mailboxes_on_user_id ON mailboxes(user_id);
CREATE TABLE IF NOT EXISTS api_tokens (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  token_digest TEXT NOT NULL UNIQUE,
  last_used_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_api_tokens_on_user_id ON api_tokens(user_id);
CREATE TABLE IF NOT EXISTS agent_grants (
  code_digest TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  challenge TEXT NOT NULL,
  token TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  unlocked_at TEXT,
  flash_notice TEXT,
  flash_alert TEXT,
  created_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_sessions_on_user_id ON sessions(user_id);
`

type Store struct {
	db *sql.DB
}

// Open connects to path, enables WAL + foreign keys, and creates the schema.
// Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	if path == ":memory:" {
		dsn = "file::memory:?_pragma=foreign_keys(ON)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN account_sub TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column name") {
		db.Close()
		return nil, fmt.Errorf("migrate account_sub: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS users_account_sub
		ON users(account_sub) WHERE account_sub != ''`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate account_sub index: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func now() string { return time.Now().UTC().Format(timeLayout) }

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseTime(raw string) (time.Time, error) {
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad timestamp %q", raw)
}

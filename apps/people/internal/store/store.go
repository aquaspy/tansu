// Package store owns SQLite access: schema, pragmas, and typed queries.
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// timeLayout matches the sibling apps so rows compare identically with
// plain string comparison (UTC).
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
CREATE TABLE IF NOT EXISTS people (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  nickname TEXT NOT NULL DEFAULT '',
  relationship TEXT NOT NULL DEFAULT '',
  birth_month INTEGER,
  birth_day INTEGER,
  birth_year INTEGER,
  emoji TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL DEFAULT '',
  ring_size TEXT NOT NULL DEFAULT '',
  shoe_size TEXT NOT NULL DEFAULT '',
  shirt_size TEXT NOT NULL DEFAULT '',
  pants_size TEXT NOT NULL DEFAULT '',
  height TEXT NOT NULL DEFAULT '',
  favorites TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_people_on_user_id ON people(user_id);
CREATE INDEX IF NOT EXISTS index_people_on_user_id_and_name ON people(user_id, name);
CREATE TABLE IF NOT EXISTS person_attrs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_person_attrs_on_person_id ON person_attrs(person_id);
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
		db.SetMaxOpenConns(1) // one connection = one shared memory database
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	// Existing databases predate the SSO column. ALTER is a no-op on
	// fresh databases; the duplicate-column error is ignored. The
	// index is created here (not in schema) because it must come
	// after the column on old databases.
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

// DB exposes the pool for transactions and tests in this package.
func (s *Store) DB() *sql.DB { return s.db }

func now() string { return time.Now().UTC().Format(timeLayout) }

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func parseTime(raw string) (time.Time, error) {
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad timestamp %q", raw)
}

// ReclaimSpace mirrors ApplicationRecord.reclaim_space; VACUUM failures are
// ignored so it never breaks a request (not even inside a transaction).
func (s *Store) ReclaimSpace() {
	_, _ = s.db.Exec("VACUUM")
}

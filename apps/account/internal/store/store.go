// Package store owns SQLite access: schema, pragmas, and typed queries.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
	_ "time/tzdata"
)

// timeLayout matches the sibling apps so rows compare identically with
// plain string comparison (UTC).
const timeLayout = "2006-01-02 15:04:05.999999"

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  password_digest TEXT NOT NULL,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_clients (
  id TEXT PRIMARY KEY,
  secret_digest TEXT NOT NULL,
  name TEXT NOT NULL,
  home TEXT NOT NULL DEFAULT '',
  icon TEXT NOT NULL DEFAULT '',
  redirect_uris TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS auth_codes (
  code_digest TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL,
  challenge TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_auth_codes_on_expires_at ON auth_codes(expires_at);
CREATE TABLE IF NOT EXISTS access_tokens (
  token_digest TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_access_tokens_on_expires_at ON access_tokens(expires_at);
CREATE TABLE IF NOT EXISTS account_links (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  linked_at TEXT NOT NULL,
  PRIMARY KEY (user_id, client_id)
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
CREATE TABLE IF NOT EXISTS pending_signups (
  email TEXT PRIMARY KEY,
  password_digest TEXT NOT NULL,
  token_digest TEXT NOT NULL UNIQUE,
  next TEXT NOT NULL DEFAULT '',
  locale TEXT NOT NULL DEFAULT 'en',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS password_resets (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  token_digest TEXT NOT NULL UNIQUE,
  locale TEXT NOT NULL DEFAULT 'en',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
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
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return st, nil
}

// migrate adds columns that postdate an existing database file.
func (s *Store) migrate() error {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('pending_signups')`)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !have["attempts"] {
		if _, err := s.db.Exec(`ALTER TABLE pending_signups ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	userCols, err := s.tableColumns("users")
	if err != nil {
		return err
	}
	if !userCols["timezone"] {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC'`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) tableColumns(table string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('` + table + `')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		have[name] = true
	}
	return have, rows.Err()
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the pool for transactions and tests in this package.
func (s *Store) DB() *sql.DB { return s.db }

func now() string { return time.Now().UTC().Format(timeLayout) }

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

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

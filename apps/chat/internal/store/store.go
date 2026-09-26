// Package store owns SQLite access: schema, pragmas, and typed queries.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// timeLayout matches the Rails datetime serialization so imported rows and
// new rows compare identically with plain string comparison (UTC).
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
CREATE TABLE IF NOT EXISTS conversations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL DEFAULT '',
  summary TEXT,
  summarized_through_id INTEGER,
  share_token TEXT UNIQUE,
  archived_at TEXT,
  model TEXT NOT NULL DEFAULT '',
  web_search INTEGER NOT NULL DEFAULT 0,
  deep_search INTEGER NOT NULL DEFAULT 0,
  effort TEXT NOT NULL DEFAULT '',
  voice_read_aloud INTEGER NOT NULL DEFAULT 0,
  voice_auto_send INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_conversations_on_user_id ON conversations(user_id);
CREATE INDEX IF NOT EXISTS index_conversations_on_user_id_and_updated_at ON conversations(user_id, updated_at);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  status TEXT,
  content TEXT,
  error TEXT,
  web INTEGER NOT NULL DEFAULT 0,
  deep INTEGER NOT NULL DEFAULT 0,
  citations TEXT,
  raw TEXT,
  token_usage TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_messages_on_conversation_id ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS index_messages_on_conversation_id_and_id ON messages(conversation_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS index_messages_one_inflight_per_conversation
  ON messages(conversation_id) WHERE role = 'assistant' AND status IN ('pending', 'streaming');
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
CREATE TABLE IF NOT EXISTS images (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  byte_size INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_images_on_message_id ON images(message_id);
CREATE TABLE IF NOT EXISTS documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  byte_size INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS index_documents_on_message_id ON documents(message_id);
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
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// migrate adds columns that postdate the database file (fresh files get
// them from schema; existing files get ALTERs).
func migrate(db *sql.DB) error {
	tables := map[string][]struct{ name, ddl string }{
		"conversations": {
			{"model", `ALTER TABLE conversations ADD COLUMN model TEXT NOT NULL DEFAULT ''`},
			{"web_search", `ALTER TABLE conversations ADD COLUMN web_search INTEGER NOT NULL DEFAULT 0`},
			{"deep_search", `ALTER TABLE conversations ADD COLUMN deep_search INTEGER NOT NULL DEFAULT 0`},
			{"effort", `ALTER TABLE conversations ADD COLUMN effort TEXT NOT NULL DEFAULT ''`},
			{"voice_read_aloud", `ALTER TABLE conversations ADD COLUMN voice_read_aloud INTEGER NOT NULL DEFAULT 0`},
			{"voice_auto_send", `ALTER TABLE conversations ADD COLUMN voice_auto_send INTEGER NOT NULL DEFAULT 1`},
		},
		"messages": {
			{"deep", `ALTER TABLE messages ADD COLUMN deep INTEGER NOT NULL DEFAULT 0`},
		},
		"users": {
			{"account_sub", `ALTER TABLE users ADD COLUMN account_sub TEXT NOT NULL DEFAULT ''`},
		},
	}
	for table, cols := range tables {
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
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
		for _, col := range cols {
			if !have[col.name] {
				if _, err := db.Exec(col.ddl); err != nil {
					return err
				}
			}
		}
	}
	// After the column exists on every database, old or new.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS users_account_sub
		ON users(account_sub) WHERE account_sub != ''`); err != nil {
		return err
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the pool for transactions and tests in this package.
func (s *Store) DB() *sql.DB { return s.db }

func now() string { return time.Now().UTC().Format(timeLayout) }

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(raw string) (time.Time, error) {
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad timestamp %q", raw)
}

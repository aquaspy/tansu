package main

import (
	"database/sql"
	"fmt"

	"github.com/aquasp/kuranotes/internal/config"
	"github.com/aquasp/kuranotes/internal/store"
)

// runImport copies a Rails-era database into the Go schema.
// Usage: kuranotes import OLD_DB_PATH
//
// IDs are preserved and password digests carry over (bcrypt is portable).
// The target database must be empty.
func runImport(cfg config.Config, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kuranotes import OLD_DB_PATH")
	}
	oldDBPath := args[0]
	st, err := openData(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	if users, err := st.ListUsers(); err != nil || len(users) > 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("target database is not empty; refusing to import")
	}
	old, err := sql.Open("sqlite", "file:"+oldDBPath+"?mode=ro")
	if err != nil {
		return err
	}
	defer old.Close()
	for _, table := range []string{"users", "notes", "api_tokens"} {
		var name string
		if err := old.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			return fmt.Errorf("not a KuraNotes database (missing %s): %w", table, err)
		}
	}
	nUsers, err := copyUsers(st, old)
	if err != nil {
		return err
	}
	nNotes, err := copyNotes(st, old)
	if err != nil {
		return err
	}
	nTokens, err := copyTokens(st, old)
	if err != nil {
		return err
	}
	fmt.Printf("imported %d users, %d notes, %d api tokens\n", nUsers, nNotes, nTokens)
	fmt.Println("note: everyone signs in again (sessions are not imported)")
	return nil
}

func copyUsers(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, email, password_digest, created_at, updated_at FROM users ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id int64
		var email, digest, created, updated string
		if err := rows.Scan(&id, &email, &digest, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO users (id, email, password_digest, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)`, id, email, digest, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyNotes(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, body, folder, preview, share_token,
		title, created_at, updated_at FROM notes ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID int64
		var body, folder, preview, title, created, updated string
		var token sql.NullString
		if err := rows.Scan(&id, &userID, &body, &folder, &preview, &token,
			&title, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO notes
			(id, user_id, body, folder, preview, share_token, title, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, body, folder, preview, token, title, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyTokens(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, name, prefix, token_digest,
		last_used_at, created_at, updated_at FROM api_tokens ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID int64
		var name, prefix, digest, created, updated string
		var lastUsed sql.NullString
		if err := rows.Scan(&id, &userID, &name, &prefix, &digest,
			&lastUsed, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO api_tokens
			(id, user_id, name, prefix, token_digest, last_used_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, name, prefix, digest, lastUsed, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

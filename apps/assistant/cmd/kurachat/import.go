package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aquasp/kurachat/internal/config"
	"github.com/aquasp/kurachat/internal/images"
	"github.com/aquasp/kurachat/internal/store"
)

// runImport copies a Rails-era database + storage dir into the Go schema.
// Usage: kurachat import OLD_DB_PATH OLD_STORAGE_DIR
//
// IDs are preserved, password digests carry over (bcrypt is portable), and
// uploaded originals are kept. Derivatives rebuild when the bytes decode;
// undecodable files (e.g. HEIC) keep the viewable original only. The target
// database must be empty.
func runImport(cfg config.Config, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: kurachat import OLD_DB_PATH OLD_STORAGE_DIR")
	}
	oldDBPath, oldStorage := args[0], args[1]
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
	for _, table := range []string{"users", "conversations", "messages"} {
		var name string
		if err := old.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			return fmt.Errorf("not a KuraChat database (missing %s): %w", table, err)
		}
	}
	nUsers, err := copyUsers(st, old)
	if err != nil {
		return err
	}
	nConvs, err := copyConversations(st, old)
	if err != nil {
		return err
	}
	nMsgs, err := copyMessages(st, old)
	if err != nil {
		return err
	}
	nImgs, warnings := copyImages(cfg, st, old, oldStorage)
	fmt.Printf("imported %d users, %d conversations, %d messages, %d images\n",
		nUsers, nConvs, nMsgs, nImgs)
	for _, w := range warnings {
		fmt.Println("warning:", w)
	}
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

func copyConversations(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, title, summary, summarized_through_id,
		share_token, archived_at, created_at, updated_at FROM conversations ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID int64
		var title, summary, token, archived, created, updated sql.NullString
		var through sql.NullInt64
		if err := rows.Scan(&id, &userID, &title, &summary, &through, &token, &archived, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO conversations
			(id, user_id, title, summary, summarized_through_id, share_token, archived_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, title, summary, through, token, archived, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyMessages(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, conversation_id, role, status, content, error, web,
		citations, raw, token_usage, created_at, updated_at FROM messages ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, convID int64
		var role string
		var status, content, errStr, citations, raw, usage, created, updated sql.NullString
		var web int
		if err := rows.Scan(&id, &convID, &role, &status, &content, &errStr, &web,
			&citations, &raw, &usage, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO messages
			(id, conversation_id, role, status, content, error, web, citations, raw, token_usage, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, convID, role, status, content, errStr, web, citations, raw, usage, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyImages(cfg config.Config, st *store.Store, old *sql.DB, oldStorage string) (int, []string) {
	var warnings []string
	var blobs string
	if err := old.QueryRow(`SELECT name FROM sqlite_master WHERE name = 'active_storage_blobs'`).Scan(&blobs); err != nil {
		return 0, warnings // no uploads at all
	}
	rows, err := old.Query(`SELECT a.record_id, b.key, b.filename, b.content_type, b.byte_size
		FROM active_storage_attachments a
		JOIN active_storage_blobs b ON b.id = a.blob_id
		WHERE a.record_type = 'Message' AND a.name = 'images' ORDER BY a.id`)
	if err != nil {
		return 0, append(warnings, err.Error())
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var msgID int64
		var key, filename, contentType string
		var byteSize int64
		if err := rows.Scan(&msgID, &key, &filename, &contentType, &byteSize); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		if len(key) < 4 {
			warnings = append(warnings, fmt.Sprintf("message %d: bad blob key", msgID))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(oldStorage, key[:2], key[2:4], key))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("message %d: %s missing (%v)", msgID, filename, err))
			continue
		}
		saved, derived, err := images.ImportBytes(cfg.DataDir, filename, contentType, raw)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("message %d: %s: %v", msgID, filename, err))
			continue
		}
		if _, err := st.CreateImage(msgID, saved.Filename, saved.ContentType, saved.ByteSize, saved.SHA256); err != nil {
			warnings = append(warnings, fmt.Sprintf("message %d: %s: %v", msgID, filename, err))
			continue
		}
		if !derived {
			warnings = append(warnings, fmt.Sprintf("message %d: %s kept without thumbnail (undecodable)", msgID, filename))
		}
		n++
	}
	if err := rows.Err(); err != nil {
		warnings = append(warnings, err.Error())
	}
	return n, warnings
}

package main

import (
	"database/sql"
	"fmt"

	"github.com/aquasp/kuracalendar/internal/config"
	"github.com/aquasp/kuracalendar/internal/store"
)

// runImport copies a Rails-era database into the Go schema.
// Usage: kuracalendar import OLD_DB_PATH
//
// IDs are preserved and password digests carry over (bcrypt is portable).
// The target database must be empty.
func runImport(cfg config.Config, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kuracalendar import OLD_DB_PATH")
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
	for _, table := range []string{"users", "events", "birthdays", "api_tokens"} {
		var name string
		if err := old.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			return fmt.Errorf("not a KuraCalendar database (missing %s): %w", table, err)
		}
	}
	nUsers, err := copyUsers(st, old)
	if err != nil {
		return err
	}
	nEvents, err := copyEvents(st, old)
	if err != nil {
		return err
	}
	nBirthdays, err := copyBirthdays(st, old)
	if err != nil {
		return err
	}
	nTokens, err := copyTokens(st, old)
	if err != nil {
		return err
	}
	fmt.Printf("imported %d users, %d events, %d birthdays, %d api tokens\n",
		nUsers, nEvents, nBirthdays, nTokens)
	fmt.Println("note: everyone signs in again (sessions are not imported)")
	return nil
}

func copyUsers(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, email, password_digest, holiday_countries,
		created_at, updated_at FROM users ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id int64
		var email, digest, packs, created, updated string
		if err := rows.Scan(&id, &email, &digest, &packs, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO users
			(id, email, password_digest, holiday_countries, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, id, email, digest, packs, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyEvents(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, title, body, all_day, starts_on,
		ends_on, starts_at, ends_at, created_at, updated_at FROM events ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID int64
		var title, body, startsOn, endsOn, created, updated string
		var allDay int
		var startsAt, endsAt sql.NullString
		if err := rows.Scan(&id, &userID, &title, &body, &allDay, &startsOn,
			&endsOn, &startsAt, &endsAt, &created, &updated); err != nil {
			return n, err
		}
		// Rails t.time columns carry a full timestamp; keep the HH:MM clock.
		if _, err := st.DB().Exec(`INSERT INTO events
			(id, user_id, title, body, all_day, starts_on, ends_on, starts_at, ends_at,
			 created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, title, body, allDay, startsOn, endsOn,
			clockOrNull(startsAt), clockOrNull(endsAt), created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// clockOrNull reduces a Rails time column ("2000-01-01 14:30:00") to HH:MM.
func clockOrNull(v sql.NullString) any {
	if !v.Valid || len(v.String) < 16 {
		return nil
	}
	if len(v.String) >= 16 && v.String[10] == ' ' {
		return v.String[11:16]
	}
	if len(v.String) >= 5 && v.String[2] == ':' {
		return v.String[:5]
	}
	return nil
}

func copyBirthdays(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, name, month, day, year, body,
		created_at, updated_at FROM birthdays ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID int64
		var name, body, created, updated string
		var month, day int
		var year sql.NullInt64
		if err := rows.Scan(&id, &userID, &name, &month, &day, &year,
			&body, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO birthdays
			(id, user_id, name, month, day, year, body, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, name, month, day, year, body, created, updated); err != nil {
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

package main

import (
	"database/sql"
	"fmt"

	"github.com/aquasp/kuraspend/internal/config"
	"github.com/aquasp/kuraspend/internal/store"
)

// runImport copies a Rails-era database into the Go schema.
// Usage: kuraspend import OLD_DB_PATH
//
// IDs are preserved and password digests carry over (bcrypt is portable).
// The target database must be empty.
func runImport(cfg config.Config, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kuraspend import OLD_DB_PATH")
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
	for _, table := range []string{"users", "expenses", "subscriptions", "payment_days", "api_tokens"} {
		var name string
		if err := old.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			return fmt.Errorf("not a KuraSpend database (missing %s): %w", table, err)
		}
	}
	nUsers, err := copyUsers(st, old)
	if err != nil {
		return err
	}
	nExpenses, err := copyExpenses(st, old)
	if err != nil {
		return err
	}
	nSubs, err := copySubscriptions(st, old)
	if err != nil {
		return err
	}
	nDays, err := copyPaymentDays(st, old)
	if err != nil {
		return err
	}
	nTokens, err := copyTokens(st, old)
	if err != nil {
		return err
	}
	fmt.Printf("imported %d users, %d expenses, %d subscriptions, %d payment days, %d api tokens\n",
		nUsers, nExpenses, nSubs, nDays, nTokens)
	fmt.Println("note: everyone signs in again (sessions are not imported)")
	return nil
}

func copyUsers(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, email, password_digest, fx, home_currency,
		income_currency, monthly_income_cents, created_at, updated_at FROM users ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, income int64
		var email, digest, fx, home, incomeCur, created, updated string
		if err := rows.Scan(&id, &email, &digest, &fx, &home,
			&incomeCur, &income, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO users
			(id, email, password_digest, fx, home_currency, income_currency,
			 monthly_income_cents, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, email, digest, fx, home, incomeCur, income, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyExpenses(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, title, amount_cents, currency,
		spent_on, category, notes, created_at, updated_at FROM expenses ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID, cents int64
		var title, currency, spentOn, category, notes, created, updated string
		if err := rows.Scan(&id, &userID, &title, &cents, &currency,
			&spentOn, &category, &notes, &created, &updated); err != nil {
			return n, err
		}
		if _, err := st.DB().Exec(`INSERT INTO expenses
			(id, user_id, title, amount_cents, currency, spent_on, category, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, title, cents, currency, spentOn, category, notes, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copySubscriptions(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, title, amount_cents, currency, interval,
		due_day, billing_month, active, notes, created_at, updated_at FROM subscriptions ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID, cents int64
		var title, currency, interval, notes, created, updated string
		var dueDay, billingMonth sql.NullInt64
		var active bool
		if err := rows.Scan(&id, &userID, &title, &cents, &currency, &interval,
			&dueDay, &billingMonth, &active, &notes, &created, &updated); err != nil {
			return n, err
		}
		activeInt := 0
		if active {
			activeInt = 1
		}
		if _, err := st.DB().Exec(`INSERT INTO subscriptions
			(id, user_id, title, amount_cents, currency, interval, due_day, billing_month,
			 active, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, title, cents, currency, interval,
			dueDay, billingMonth, activeInt, notes, created, updated); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func copyPaymentDays(st *store.Store, old *sql.DB) (int, error) {
	rows, err := old.Query(`SELECT id, user_id, title, due_day, active, notes,
		created_at, updated_at FROM payment_days ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, userID int64
		var title, notes, created, updated string
		var dueDay int64
		var active bool
		if err := rows.Scan(&id, &userID, &title, &dueDay, &active,
			&notes, &created, &updated); err != nil {
			return n, err
		}
		activeInt := 0
		if active {
			activeInt = 1
		}
		if _, err := st.DB().Exec(`INSERT INTO payment_days
			(id, user_id, title, due_day, active, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, title, dueDay, activeInt, notes, created, updated); err != nil {
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

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aquasp/kuraspend/internal/config"
	"github.com/aquasp/kuraspend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func openData(cfg config.Config) (*store.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(cfg.DataDir, "kuraspend.sqlite3"))
}

func runUsers(cfg config.Config) error {
	st, err := openData(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	users, err := st.ListUsers()
	if err != nil {
		return err
	}
	fmt.Printf("%-4s %-40s %-8s %-5s %-5s %s\n", "id", "email", "expenses", "days", "subs", "created")
	for _, u := range users {
		fmt.Printf("%-4d %-40s %-8d %-5d %-5d %s\n", u.ID, u.Email,
			st.CountExpenses(u.ID), st.CountPaymentDays(u.ID), st.CountSubscriptions(u.ID),
			u.CreatedAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func runCreate(cfg config.Config) error {
	email := os.Getenv("EMAIL")
	password := os.Getenv("PASSWORD")
	if !store.ValidEmail(email) {
		return fmt.Errorf("EMAIL invalid (export EMAIL=you@example.com)")
	}
	if len(password) < 8 {
		return fmt.Errorf("PASSWORD too short (export PASSWORD=..., min 8 chars)")
	}
	st, err := openData(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	digest, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	user, err := st.CreateUser(email, string(digest))
	if err != nil {
		return err
	}
	fmt.Printf("created user %d %s\n", user.ID, user.Email)
	return nil
}

func runPassword(cfg config.Config) error {
	email := os.Getenv("EMAIL")
	password := os.Getenv("PASSWORD")
	if !store.ValidEmail(email) {
		return fmt.Errorf("EMAIL invalid (export EMAIL=you@example.com)")
	}
	if len(password) < 8 {
		return fmt.Errorf("PASSWORD too short (export PASSWORD=..., min 8 chars)")
	}
	st, err := openData(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	user, err := st.FindUserByEmail(email)
	if err != nil {
		return err
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	if err := st.UpdateUserPassword(user.ID, string(digest)); err != nil {
		return err
	}
	fmt.Printf("password reset for %s\n", user.Email)
	return nil
}

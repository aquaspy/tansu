package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aquasp/kuranotes/internal/config"
	"github.com/aquasp/kuranotes/internal/handler"
	"github.com/aquasp/kuranotes/internal/store"
)

func runServe(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "kuranotes.sqlite3"))
	if err != nil {
		return err
	}
	defer st.Close()

	// Boot sweep: dead sessions are collected. The ticker repeats it
	// every 5 minutes.
	_ = st.DeleteStaleSessions(store.SessionMaxAge)
	_ = st.DeleteStaleKuraLogins(store.KuraLoginTTL)
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			_ = st.DeleteStaleSessions(store.SessionMaxAge)
			_ = st.DeleteStaleKuraLogins(store.KuraLoginTTL)
		}
	}()

	srv := handler.NewServer(cfg, st)
	addr := cfg.ListenAddr()
	fmt.Println("kuranotes listening on", addr)
	return http.ListenAndServe(addr, srv.Routes())
}

package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aquasp/kuracalendar/internal/config"
	"github.com/aquasp/kuracalendar/internal/handler"
	"github.com/aquasp/kuracalendar/internal/store"
)

func runServe(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "kuracalendar.sqlite3"))
	if err != nil {
		return err
	}
	defer st.Close()

	// Boot sweep: dead sessions are collected. The ticker repeats it
	// every 5 minutes.
	_ = st.DeleteStaleSessions(store.SessionMaxAge)
	_ = st.DeleteStaleKuraLogins(store.KuraLoginTTL)
	_ = st.DeleteStaleAgentGrants()
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			_ = st.DeleteStaleSessions(store.SessionMaxAge)
			_ = st.DeleteStaleKuraLogins(store.KuraLoginTTL)
	_ = st.DeleteStaleAgentGrants()
		}
	}()

	srv := handler.NewServer(cfg, st)
	addr := cfg.ListenAddr()
	fmt.Println("kuracalendar listening on", addr)
	return http.ListenAndServe(addr, srv.Routes())
}

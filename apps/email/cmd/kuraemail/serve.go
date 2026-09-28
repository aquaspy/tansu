package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aquasp/kuraemail/internal/config"
	"github.com/aquasp/kuraemail/internal/handler"
	"github.com/aquasp/kuraemail/internal/store"
)

func runServe(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "kuraemail.sqlite3"))
	if err != nil {
		return err
	}
	defer st.Close()

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
	fmt.Println("kuraemail listening on", addr)
	return http.ListenAndServe(addr, srv.Routes())
}

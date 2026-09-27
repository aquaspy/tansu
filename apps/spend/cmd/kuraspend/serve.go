package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aquasp/kuraspend/internal/config"
	"github.com/aquasp/kuraspend/internal/fx"
	"github.com/aquasp/kuraspend/internal/handler"
	"github.com/aquasp/kuraspend/internal/store"
)

func runServe(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "kuraspend.sqlite3"))
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

	// Live quotes refresh at boot (async, best-effort) and every 6 hours.
	// Failures only log: the manual rates stand in until a fetch lands.
	refreshFX := func() {
		if n, err := fx.Refresh(context.Background(), st, fx.DefaultClient()); err != nil {
			fmt.Println("fx refresh:", err)
		} else {
			fmt.Printf("fx refreshed %d quotes\n", n)
		}
	}
	go refreshFX()
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for range t.C {
			refreshFX()
		}
	}()

	srv := handler.NewServer(cfg, st)
	addr := cfg.ListenAddr()
	fmt.Println("kuraspend listening on", addr)
	return http.ListenAndServe(addr, srv.Routes())
}

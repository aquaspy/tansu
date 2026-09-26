package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aquasp/kuraaccount/internal/config"
	"github.com/aquasp/kuraaccount/internal/handler"
	"github.com/aquasp/kuraaccount/internal/store"
)

func runServe(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	// Fail closed on a malformed client registry.
	if err := cfg.ParseClients(); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "kuraaccount.sqlite3"))
	if err != nil {
		return err
	}
	defer st.Close()

	seeds := make([]store.SeedClient, 0, len(cfg.Clients))
	for _, c := range cfg.Clients {
		seeds = append(seeds, store.SeedClient{
			ID: c.ID, Secret: c.Secret, Name: c.Name,
			Home: c.Home, Icon: c.Icon, RedirectURIs: c.RedirectURIs,
		})
	}
	if err := st.SeedClients(seeds); err != nil {
		return err
	}

	// Boot sweep: dead sessions, codes, and tokens are collected. The
	// ticker repeats it every 5 minutes.
	_ = st.DeleteStaleSessions(store.SessionMaxAge)
	st.DeleteStaleOAuth()
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			_ = st.DeleteStaleSessions(store.SessionMaxAge)
			st.DeleteStaleOAuth()
		}
	}()

	srv := handler.NewServer(cfg, st)
	addr := cfg.ListenAddr()
	fmt.Println("kuraaccount listening on", addr)
	return http.ListenAndServe(addr, srv.Routes())
}

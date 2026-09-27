package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aquasp/kurachat/internal/chat"
	"github.com/aquasp/kurachat/internal/config"
	"github.com/aquasp/kurachat/internal/handler"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/suite"
)

func suiteApps(cfg config.Config) []suite.App {
	var out []suite.App
	for _, pair := range []struct{ name, base string }{
		{"notes", cfg.NotesURL},
		{"calendar", cfg.CalendarURL},
		{"spend", cfg.SpendURL},
		{"people", cfg.PeopleURL},
	} {
		if pair.base != "" {
			out = append(out, suite.App{Name: pair.name, Base: pair.base})
		}
	}
	return out
}

func runServe(cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "kurachat.sqlite3"))
	if err != nil {
		return err
	}
	defer st.Close()

	svc := &chat.Service{
		Store:   st,
		Hub:     chat.NewHub(),
		DataDir: cfg.DataDir,
		Config: chat.CompleterConfig{
			Model:            cfg.OpenRouterModel,
			Models:           cfg.OpenRouterModels,
			Effort:           cfg.OpenRouterReasoningEffort,
			WindowTokens:     cfg.ChatWindowTokens,
			KeepRecentTokens: cfg.ChatKeepRecentTokens,
			ReplyMaxTokens:   cfg.ChatReplyMaxTokens,
			PDFEngine:        cfg.OpenRouterPDFEngine,
			Search: chat.SearchConfig{
				Enabled:        cfg.SearchEnabled,
				Engine:         cfg.SearchEngine,
				Mode:           cfg.SearchMode,
				MaxResults:     cfg.SearchMaxResults,
				DeepMode:       cfg.SearchDeepMode,
				DeepMaxResults: cfg.SearchDeepMaxResults,
				FeeIncluded:    cfg.SearchFeeIncluded,
			},
		},
		APIKey:    cfg.OpenRouterAPIKey,
		Catalog:   openrouter.NewCatalog("", cfg.OpenRouterAPIKey),
		SuiteKey:  cfg.AppsKey,
		SuiteApps: suiteApps(cfg),
		Zone:      cfg.TimeZone,
		ToolModel: cfg.ToolModel,
	}
	// Boot sweep: completions orphaned by a restart fail fast, and dead
	// sessions are collected. The ticker repeats both every 5 minutes.
	svc.SweepStale()
	_ = st.DeleteStaleSessions(store.SessionMaxAge)
	_ = st.DeleteStaleKuraLogins(store.KuraLoginTTL)
	_ = st.DeleteStaleAppConnects(store.AppConnectTTL)
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			svc.SweepStale()
			_ = st.DeleteStaleSessions(store.SessionMaxAge)
			_ = st.DeleteStaleKuraLogins(store.KuraLoginTTL)
	_ = st.DeleteStaleAppConnects(store.AppConnectTTL)
		}
	}()

	srv := handler.NewServer(cfg, st, svc)
	addr := cfg.ListenAddr()
	fmt.Println("kurachat listening on", addr)
	return http.ListenAndServe(addr, srv.Routes())
}

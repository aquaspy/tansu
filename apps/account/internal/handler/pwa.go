package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aquasp/kuraaccount/internal/i18n"
)

func (s *Server) handleUp(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("up"))
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	manifest := map[string]any{
		"name":       "TansuAccount",
		"short_name": "TansuAccount",
		"icons": []map[string]string{
			{"src": "/icon.png", "type": "image/png", "sizes": "512x512", "purpose": "any"},
			{"src": "/icon.png", "type": "image/png", "sizes": "512x512", "purpose": "maskable"},
			{"src": "/icon.svg", "type": "image/svg+xml", "sizes": "any", "purpose": "any"},
		},
		"start_url":        "/",
		"display":          "standalone",
		"scope":            "/",
		"description":      i18n.T(LocaleOf(r), "pwa.description"),
		"theme_color":      "#0a0d12",
		"background_color": "#0a0d12",
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	_ = json.NewEncoder(w).Encode(manifest)
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(s.WebDir, "sw.js"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(raw)
}

func (s *Server) handleStaticFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(s.WebDir, name))
	}
}

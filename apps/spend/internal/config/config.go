// Package config reads the process environment into a typed Config.
package config

import (
	"net"
	"os"
	"strings"
)

type Config struct {
	Bind          string
	DataDir       string
	KuraHosts     []string
	SignupEnabled bool
	ForceSSL      bool

	// Kura Account SSO (optional). When the account URL and client
	// secret are set, the app offers "Entrar com Tansu" alongside
	// the standalone password login. Nothing else changes.
	KuraAccountURL   string
	KuraClientID     string
	KuraClientSecret string

	// KuraAssistantURL is the Assistant origin allowed to finish an app link.
	// Empty disables /agent/connect.
	KuraAssistantURL string

	// CalendarURL + SyncSecret push payment days into Tansu Calendar.
	// Both empty keeps Spend standalone. Subscriptions are not synced.
	CalendarURL string
	SyncSecret  string
}

func Load() Config {
	return Config{
		Bind:          envOr("BIND", "127.0.0.1:3004"),
		DataDir:       envOr("DATA_DIR", "storage"),
		KuraHosts:     splitList(os.Getenv("KURA_HOST")),
		SignupEnabled: flag("SIGNUP_ENABLED", true),
		ForceSSL:      flag("FORCE_SSL", false),

		KuraAccountURL:   strings.TrimSuffix(envOr("KURA_ACCOUNT_URL", ""), "/"),
		KuraClientID:     envOr("KURA_CLIENT_ID", "kuraspend"),
		KuraClientSecret: envOr("KURA_CLIENT_SECRET", ""),
		KuraAssistantURL: strings.TrimRight(strings.TrimSpace(os.Getenv("KURA_ASSISTANT_URL")), "/"),
		CalendarURL:      strings.TrimSuffix(envOr("KURA_CALENDAR_URL", ""), "/"),
		SyncSecret:       envOr("KURA_SYNC_SECRET", ""),
	}
}

// SyncEnabled reports whether payment-day push to Calendar is configured.
func (c Config) SyncEnabled() bool {
	return c.CalendarURL != "" && c.SyncSecret != ""
}

// AccountEnabled reports whether Kura Account SSO is configured.
func (c Config) AccountEnabled() bool {
	return c.KuraAccountURL != "" && c.KuraClientSecret != ""
}

// ListenAddr splits BIND into host:port for net.Listen. Compose sets
// BIND=127.0.0.1:3004; the Docker image overrides the port mapping.
func (c Config) ListenAddr() string {
	host, port, err := net.SplitHostPort(c.Bind)
	if err != nil {
		return c.Bind
	}
	return net.JoinHostPort(host, port)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func flag(key string, def bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch raw {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

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
}

func Load() Config {
	return Config{
		Bind:          envOr("BIND", "127.0.0.1:3003"),
		DataDir:       envOr("DATA_DIR", "storage"),
		KuraHosts:     splitList(os.Getenv("KURA_HOST")),
		SignupEnabled: flag("SIGNUP_ENABLED", true),
		ForceSSL:      flag("FORCE_SSL", false),

		KuraAccountURL:   strings.TrimSuffix(envOr("KURA_ACCOUNT_URL", ""), "/"),
		KuraClientID:     envOr("KURA_CLIENT_ID", "kuracalendar"),
		KuraClientSecret: envOr("KURA_CLIENT_SECRET", ""),
	}
}

// AccountEnabled reports whether Kura Account SSO is configured.
func (c Config) AccountEnabled() bool {
	return c.KuraAccountURL != "" && c.KuraClientSecret != ""
}

// ListenAddr splits BIND into host:port for net.Listen. Compose sets
// BIND=127.0.0.1:3003; the Docker image overrides the port mapping.
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

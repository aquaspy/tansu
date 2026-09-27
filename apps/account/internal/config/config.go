// Package config reads the process environment into a typed Config.
package config

import (
	"encoding/json"
	"fmt"
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
	// ClientsJSON is the static first-party client registry:
	// [{"id","secret","name","home","icon","redirect_uris":[]}].
	ClientsJSON string
	Clients     []ClientConfig
	// ResendAPIKey enables signup confirmation and password reset.
	// Empty keeps immediate signup and hides the forgot-password link.
	ResendAPIKey string
	// ResendFrom is the verified sender. Required when ResendAPIKey is set.
	ResendFrom string
}

// ClientConfig is one suite app allowed to use "Entrar com Tansu".
type ClientConfig struct {
	ID           string   `json:"id"`
	Secret       string   `json:"secret"`
	Name         string   `json:"name"`
	Home         string   `json:"home"`
	Icon         string   `json:"icon"`
	RedirectURIs []string `json:"redirect_uris"`
}

func Load() Config {
	return Config{
		Bind:          envOr("BIND", "127.0.0.1:3006"),
		DataDir:       envOr("DATA_DIR", "storage"),
		KuraHosts:     splitList(os.Getenv("KURA_HOST")),
		SignupEnabled: flag("SIGNUP_ENABLED", true),
		ForceSSL:      flag("FORCE_SSL", false),
		ClientsJSON:   strings.TrimSpace(os.Getenv("KURA_CLIENTS_JSON")),
		ResendAPIKey:  strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		ResendFrom:    strings.TrimSpace(os.Getenv("RESEND_FROM")),
	}
}

// MailEnabled reports whether Resend is configured. Signup then waits
// for an inbox link, and login offers password reset. Self-host installs
// leave the key empty: signup is immediate and reset stays on the shell.
func (c Config) MailEnabled() bool { return c.ResendAPIKey != "" }

// Validate fails closed when mail is configured without a usable sender.
func (c Config) Validate() error {
	if c.MailEnabled() && !validSender(c.ResendFrom) {
		return fmt.Errorf("RESEND_FROM must be an email address when RESEND_API_KEY is set")
	}
	return nil
}

// validSender accepts "ada@example.com" and "Tansu <ada@example.com>".
func validSender(from string) bool {
	addr := strings.TrimSpace(from)
	if i := strings.LastIndex(addr, "<"); i >= 0 {
		addr = strings.TrimSpace(strings.TrimSuffix(addr[i+1:], ">"))
	}
	at := strings.Index(addr, "@")
	if at <= 0 || strings.Contains(addr[at+1:], "@") {
		return false
	}
	dot := strings.LastIndex(addr, ".")
	return dot > at+1 && dot < len(addr)-1 && !strings.ContainsAny(addr, " \t")
}

// ParseClients decodes and validates the static registry. No clients
// is valid (login local + hub vazio); malformed JSON fails closed.
func (c *Config) ParseClients() error {
	if c.ClientsJSON == "" {
		c.Clients = nil
		return nil
	}
	var list []ClientConfig
	dec := json.NewDecoder(strings.NewReader(c.ClientsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&list); err != nil {
		return fmt.Errorf("KURA_CLIENTS_JSON: %w", err)
	}
	seen := map[string]bool{}
	for i := range list {
		cl := &list[i]
		cl.ID = strings.TrimSpace(cl.ID)
		cl.Name = strings.TrimSpace(cl.Name)
		cl.Home = strings.TrimSpace(cl.Home)
		if cl.ID == "" || len(cl.Secret) < 16 || cl.Name == "" || len(cl.RedirectURIs) == 0 {
			return fmt.Errorf("KURA_CLIENTS_JSON: client %d precisa de id, secret>=16, name e redirect_uris", i)
		}
		for _, uri := range cl.RedirectURIs {
			if strings.TrimSpace(uri) == "" {
				return fmt.Errorf("KURA_CLIENTS_JSON: client %q tem redirect vazio", cl.ID)
			}
		}
		if seen[cl.ID] {
			return fmt.Errorf("KURA_CLIENTS_JSON: client %q duplicado", cl.ID)
		}
		seen[cl.ID] = true
	}
	c.Clients = list
	return nil
}

// ListenAddr splits BIND into host:port for net.Listen. Compose sets
// BIND=127.0.0.1:3006; the Docker image overrides the port mapping.
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

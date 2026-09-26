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
	}
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

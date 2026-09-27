// Package config reads the process environment into a typed Config.
// CHAT_* names match the Rails app; provider variables are OpenRouter's.
package config

import (
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
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

	OpenRouterAPIKey          string
	OpenRouterModel           string // resolved default: Models[0]
	OpenRouterModels          []string
	TierCheapMax              float64           // blended $/1M at/below: cheap (default 1)
	TierExpensiveMin          float64           // blended $/1M at/above: expensive (default 10)
	TierPins                  map[string]string // model id -> cheap|medium|expensive override
	OpenRouterReasoningEffort string
	OpenRouterPDFEngine       string // file-parser engine: mistral-ocr (default), cloudflare-ai, native
	LibreOfficeBin            string // soffice lookup for docx/pptx conversion (default "soffice")

	VoiceSTTModel   string // transcriptions model (default whisper turbo)
	VoiceTTSModel   string // speech model (default Azure mai-voice-2)
	VoiceTTSVoice   string // TTS voice id (default pt-BR-FranciscaNeural)
	VoiceTTSVoiceEN string // TTS voice for English text (default en-US-AvaNeural)

	SearchEnabled        bool
	SearchEngine         string // exa (default), native, parallel, perplexity, firecrawl
	SearchMode           string // engine mode: exa auto (default), fast, deep-lite, ...
	SearchMaxResults     int
	SearchDeepMode       string // deep toggle mode: exa deep-lite (default), deep, deep-reasoning
	SearchDeepMaxResults int
	SearchFeeIncluded    bool // search fee already folded into usage.cost (default true)

	ChatWindowTokens     int
	ChatKeepRecentTokens int
	ChatReplyMaxTokens   int // 0 = no cap

	// Sibling apps the Assistant can act on. Empty URL hides that app.
	// AppsKey encrypts the per-user tokens; nil disables linking.
	NotesURL    string
	CalendarURL string
	SpendURL    string
	PeopleURL   string
	AppsKey     []byte
	TimeZone    *time.Location
	ToolModel   string
}

func Load() Config {
	return Config{
		Bind:          envOr("BIND", "127.0.0.1:3000"),
		DataDir:       envOr("DATA_DIR", "storage"),
		KuraHosts:     splitList(os.Getenv("KURA_HOST")),
		SignupEnabled: flag("SIGNUP_ENABLED", true),
		ForceSSL:      flag("FORCE_SSL", false),

		KuraAccountURL:   strings.TrimSuffix(envOr("KURA_ACCOUNT_URL", ""), "/"),
		KuraClientID:     envOr("KURA_CLIENT_ID", "kurachat"),
		KuraClientSecret: envOr("KURA_CLIENT_SECRET", ""),

		OpenRouterAPIKey:          strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		OpenRouterModel:           defaultModel(),
		OpenRouterModels:          modelList(),
		TierCheapMax:              floatEnv("OPENROUTER_TIER_CHEAP_MAX", 1),
		TierExpensiveMin:          floatEnv("OPENROUTER_TIER_EXPENSIVE_MIN", 10),
		TierPins:                  tierPins(),
		OpenRouterReasoningEffort: envOr("OPENROUTER_REASONING_EFFORT", "xhigh"),
		OpenRouterPDFEngine:       envOr("OPENROUTER_PDF_ENGINE", "mistral-ocr"),
		LibreOfficeBin:            envOr("LIBREOFFICE_BIN", "soffice"),

		VoiceSTTModel:   envOr("OPENROUTER_STT_MODEL", "openai/whisper-large-v3-turbo"),
		VoiceTTSModel:   envOr("OPENROUTER_TTS_MODEL", "microsoft/mai-voice-2"),
		VoiceTTSVoice:   envOr("OPENROUTER_TTS_VOICE", "pt-BR-FranciscaNeural"),
		VoiceTTSVoiceEN: envOr("OPENROUTER_TTS_VOICE_EN", "en-US-AvaNeural"),

		SearchEnabled:        flag("SEARCH_ENABLED", true),
		SearchEngine:         envOr("SEARCH_ENGINE", "exa"),
		SearchMode:           envOr("SEARCH_MODE", "auto"),
		SearchMaxResults:     clampInt(intEnv("SEARCH_MAX_RESULTS", 5), 1, 25),
		SearchDeepMode:       envOr("SEARCH_DEEP_MODE", "deep-lite"),
		SearchDeepMaxResults: clampInt(intEnv("SEARCH_DEEP_MAX_RESULTS", 10), 1, 25),
		SearchFeeIncluded:    flag("SEARCH_FEE_INCLUDED", true),

		ChatWindowTokens:     intEnv("CHAT_WINDOW_TOKENS", 150000),
		ChatKeepRecentTokens: intEnv("CHAT_KEEP_RECENT_TOKENS", 32000),
		ChatReplyMaxTokens:   intEnv("CHAT_REPLY_MAX_TOKENS", 0),

		NotesURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("KURA_NOTES_URL")), "/"),
		CalendarURL: strings.TrimRight(strings.TrimSpace(os.Getenv("KURA_CALENDAR_URL")), "/"),
		SpendURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("KURA_SPEND_URL")), "/"),
		PeopleURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("KURA_PEOPLE_URL")), "/"),
		AppsKey:     decodeKey(os.Getenv("KURA_APPS_KEY")),
		TimeZone:    loadZone(os.Getenv("KURA_TIMEZONE")),
		ToolModel:   strings.TrimSpace(os.Getenv("KURA_TOOL_MODEL")),
	}
}

func decodeKey(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}

func loadZone(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "America/Sao_Paulo"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc, _ = time.LoadLocation("America/Sao_Paulo")
	}
	return loc
}

// AccountEnabled reports whether Kura Account SSO is configured.
func (c Config) AccountEnabled() bool {
	return c.KuraAccountURL != "" && c.KuraClientSecret != ""
}

// ListenAddr splits BIND into host:port for net.Listen. Compose sets
// BIND=127.0.0.1:3000; the Docker image overrides the port mapping.
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

func intEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func floatEnv(key string, def float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return n
}

func flag(key string, def bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// modelList resolves the configured models: OPENROUTER_MODELS wins when
// set, else the single OPENROUTER_MODEL, else the deploy defaults.
// First entry is the default.
func modelList() []string {
	if models := dedupe(splitList(os.Getenv("OPENROUTER_MODELS"))); len(models) > 0 {
		return models
	}
	if m := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")); m != "" {
		return []string{m}
	}
	return defaultModels()
}

// defaultModels is the deploy default menu: luna first (cheap default),
// then the flagship and frontier-lab models.
func defaultModels() []string {
	return []string{
		"openai/gpt-6-luna",
		"deepseek/deepseek-v4.1-flash",
		"anthropic/claude-opus-5.5",
		"meta/muse-glimmer-30b",
		"x-ai/grok-4.7",
	}
}

// tierPins parses OPENROUTER_TIER_PINS ("id:tier,...") into per-model
// tier overrides. Malformed entries and unknown tiers are dropped.
func tierPins() map[string]string {
	out := map[string]string{}
	for _, entry := range splitList(os.Getenv("OPENROUTER_TIER_PINS")) {
		id, tier, ok := strings.Cut(entry, ":")
		if !ok {
			continue
		}
		id, tier = strings.TrimSpace(id), strings.ToLower(strings.TrimSpace(tier))
		if id == "" {
			continue
		}
		switch tier {
		case "cheap", "medium", "expensive":
			out[id] = tier
		}
	}
	return out
}

func defaultModel() string { return modelList()[0] }

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

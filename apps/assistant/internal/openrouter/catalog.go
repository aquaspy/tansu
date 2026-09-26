package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// catalogTTL bounds how stale model capability data may be. Modalities
// change only when providers ship new models; a miss fails open anyway.
const catalogTTL = 24 * time.Hour

// Catalog answers model capability questions from GET /models (a public
// endpoint), cached per base URL. A nil *Catalog anywhere upstream means
// "unknown": callers fail open and never touch the network.
type Catalog struct {
	base string
	key  string
	http *http.Client

	mu      sync.Mutex
	fetched time.Time
	vision  map[string]bool    // model id -> accepts image input
	prices  map[string]float64 // model id -> blended $/1M tokens
}

// Price tiers for the model picker, derived from blended $/1M tokens.
const (
	TierCheap     = "cheap"
	TierMedium    = "medium"
	TierExpensive = "expensive"
)

// PriceTier buckets a blended $/1M-tokens price: at or below cheapMax is
// cheap, at or above expensiveMin is expensive, anything between is
// medium.
func PriceTier(blended, cheapMax, expensiveMin float64) string {
	if blended <= cheapMax {
		return TierCheap
	}
	if blended >= expensiveMin {
		return TierExpensive
	}
	return TierMedium
}

// NewCatalog builds a capability cache. Empty base selects the production
// API (tests point it at a fake server).
func NewCatalog(base, apiKey string) *Catalog {
	if base == "" {
		base = baseURL
	}
	return &Catalog{base: base, key: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

// SupportsImage reports whether modelID accepts image input. Unknown ids
// (aliases, new models) and lookup failures fail open: sending images to
// a text-only model errors loudly at the provider, while dropping them on
// an unknown model would silently lose user data.
func (c *Catalog) SupportsImage(ctx context.Context, modelID string) bool {
	if c == nil {
		return true
	}
	c.ensure(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.vision[modelID]; ok {
		return v
	}
	return true // unknown id under a fresh catalog: fail open, no refetch
}

// BlendedPrice reports the model's blended text price in $/1M tokens,
// the mean of per-token prompt and completion prices. Unknown ids and
// lookup failures report ok=false so callers can fail open (no dot).
func (c *Catalog) BlendedPrice(ctx context.Context, modelID string) (float64, bool) {
	if c == nil {
		return 0, false
	}
	c.ensure(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.prices[modelID]
	return p, ok
}

// ensure refreshes the cached catalog when stale. Failures keep the
// previous snapshot (possibly empty); lookups fail open either way.
func (c *Catalog) ensure(ctx context.Context) {
	c.mu.Lock()
	fresh := time.Since(c.fetched) < catalogTTL
	c.mu.Unlock()
	if fresh {
		return
	}
	vision, prices, err := c.fetch(ctx)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.vision, c.prices, c.fetched = vision, prices, time.Now()
	c.mu.Unlock()
}

func (c *Catalog) fetch(ctx context.Context) (map[string]bool, map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return nil, nil, err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, &Error{Msg: "catalog_unavailable", Code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, err
	}
	var decoded struct {
		Data []struct {
			ID           string `json:"id"`
			Architecture struct {
				Modality string `json:"modality"`
			} `json:"architecture"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, nil, err
	}
	vision := make(map[string]bool, len(decoded.Data))
	prices := make(map[string]float64, len(decoded.Data))
	for _, m := range decoded.Data {
		if m.ID == "" {
			continue
		}
		if m.Architecture.Modality != "" {
			in, _, _ := strings.Cut(m.Architecture.Modality, "->")
			vision[m.ID] = strings.Contains(in, "image")
		}
		if p, ok := blendedPer1M(m.Pricing.Prompt, m.Pricing.Completion); ok {
			prices[m.ID] = p
		}
	}
	return vision, prices, nil
}

// blendedPer1M converts OpenRouter's per-token price strings to a blended
// $/1M-tokens figure (mean of prompt and completion). Unparseable pairs
// report ok=false; a free model ("0"/"0") is a valid 0.
func blendedPer1M(prompt, completion string) (float64, bool) {
	p, err := strconv.ParseFloat(strings.TrimSpace(prompt), 64)
	if err != nil {
		return 0, false
	}
	co, err := strconv.ParseFloat(strings.TrimSpace(completion), 64)
	if err != nil {
		return 0, false
	}
	return (p + co) / 2 * 1e6, true
}

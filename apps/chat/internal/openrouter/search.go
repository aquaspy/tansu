package openrouter

import "strings"

// SearchOptions configures one web-search request via the `web` plugin.
// Zero values are omitted from the payload (OpenRouter applies its own
// defaults); KuraChat always passes fully-resolved values.
type SearchOptions struct {
	Engine     string // exa, native, parallel, perplexity, firecrawl
	Mode       string // engine mode, e.g. exa "auto"
	MaxResults int    // 0 = OpenRouter default
}

// plugin renders the {"id": "web", ...} payload.
func (o *SearchOptions) plugin() map[string]any {
	p := map[string]any{"id": "web"}
	if o == nil {
		return p
	}
	if o.Engine != "" {
		p["engine"] = o.Engine
	}
	if o.Mode != "" {
		p["mode"] = o.Mode
	}
	if o.MaxResults > 0 {
		p["max_results"] = o.MaxResults
	}
	return p
}

// Citation is one url_citation annotation recovered from a response event.
type Citation struct {
	URL   string
	Title string
}

// AnnotationCitations scans one response event (stream chunk or completed
// response) for url_citation annotations, deduped by URL in order.
// OpenRouter standardizes native and Exa results to this schema.
func AnnotationCitations(event map[string]any) []Citation {
	var out []Citation
	seen := map[string]bool{}
	add := func(raw any) {
		arr, _ := raw.([]any)
		for _, item := range arr {
			ann, _ := item.(map[string]any)
			if ann == nil {
				continue
			}
			inner, _ := ann["url_citation"].(map[string]any)
			if inner == nil {
				continue
			}
			u, _ := inner["url"].(string)
			u = strings.TrimSpace(u)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			title, _ := inner["title"].(string)
			out = append(out, Citation{URL: u, Title: strings.TrimSpace(title)})
		}
	}
	if choices, _ := event["choices"].([]any); choices != nil {
		for _, c := range choices {
			choice, _ := c.(map[string]any)
			if choice == nil {
				continue
			}
			if delta, _ := choice["delta"].(map[string]any); delta != nil {
				add(delta["annotations"])
			}
			if msg, _ := choice["message"].(map[string]any); msg != nil {
				add(msg["annotations"])
			}
		}
	}
	if msg, _ := event["message"].(map[string]any); msg != nil {
		add(msg["annotations"])
	}
	return out
}

// Efforts lists the reasoning effort values OpenRouter accepts.
var Efforts = []string{"none", "low", "medium", "high", "xhigh", "max"}

// ValidEffort reports whether v is a known effort ("" = server default).
func ValidEffort(v string) bool {
	if v == "" {
		return true
	}
	for _, e := range Efforts {
		if v == e {
			return true
		}
	}
	return false
}

// SearchFeeUSD returns the per-request OpenRouter-credits fee for a web
// plugin call, or ok=false when the fee is unknown (native passthrough)
// or billed outside OpenRouter (BYOK Firecrawl). Rates follow
// https://openrouter.ai/docs/features/web-search (2026-09); they only
// affect totals when SEARCH_FEE_INCLUDED=false, otherwise the fee is
// already inside usage.cost.
func SearchFeeUSD(engine, mode string, maxResults int) (fee float64, ok bool) {
	extra := 0.0
	if maxResults > 10 {
		extra = 0.001 * float64(maxResults-10)
	}
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "exa", "":
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "deep-lite", "deep":
			return 0.012 + extra, true
		case "deep-reasoning":
			return 0.015 + extra, true
		default: // instant, fast, auto (and unknown: OpenRouter defaults to auto)
			return 0.007 + extra, true
		}
	case "parallel":
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "turbo", "fast":
			return 0.001 + extra, true
		default: // basic, advanced
			return 0.005 + extra, true
		}
	case "perplexity":
		return 0.005, true
	default: // native (provider passthrough), firecrawl (BYOK), unknown
		return 0, false
	}
}

package chat

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/aquasp/kurachat/internal/views"
)

// tokenUsageFrom reduces a completion's usage block to the stored shape:
// raw token counts plus the provider's billed total when present.
func tokenUsageFrom(response map[string]any) map[string]any {
	usage, _ := response["usage"].(map[string]any)
	if usage == nil {
		return map[string]any{}
	}
	inputDetails, _ := firstMap(usage["prompt_tokens_details"], usage["input_tokens_details"])
	outputDetails, _ := firstMap(usage["completion_tokens_details"], usage["output_tokens_details"])
	out := map[string]any{}
	setNum := func(key string, vals ...any) {
		for _, v := range vals {
			if v != nil {
				if f, ok := numVal(v); ok {
					out[key] = f
				}
				return
			}
		}
	}
	setNum("input_tokens", usage["prompt_tokens"], usage["input_tokens"])
	if inputDetails != nil {
		setNum("cached_tokens", inputDetails["cached_tokens"])
	}
	setNum("output_tokens", usage["completion_tokens"], usage["output_tokens"])
	if outputDetails != nil {
		setNum("reasoning_tokens", outputDetails["reasoning_tokens"])
	}
	if v, ok := usage["cost"]; ok && v != nil {
		if f, ok := numVal(v); ok {
			out["cost_usd"] = f
		}
	}
	if model, _ := response["model"].(string); model != "" {
		out["model"] = model
	}
	return out
}

func firstMap(vals ...any) (map[string]any, bool) {
	for _, v := range vals {
		if m, ok := v.(map[string]any); ok {
			return m, true
		}
	}
	return nil, false
}

func numVal(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// safeCitationURL keeps http(s) URLs only (safe_citation_url).
func safeCitationURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

// StoredCitations parses + revalidates the citations JSON column.
func StoredCitations(blob string) []views.Citation {
	if blob == "" {
		return nil
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(blob), &arr); err != nil {
		return nil
	}
	var out []views.Citation
	for _, c := range arr {
		rawURL, _ := c["url"].(string)
		title, _ := c["title"].(string)
		if u := safeCitationURL(rawURL); u != "" {
			out = append(out, views.Citation{Title: strings.TrimSpace(title), URL: u})
		}
	}
	return out
}

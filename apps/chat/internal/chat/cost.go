package chat

import (
	"encoding/json"
	"fmt"
)

// Cost: the provider's billed total wins; legacy xAI rows carry ticks;
// rows with token counts only contribute nothing (no invented prices).
const ticksPerUSD = 10_000_000_000.0

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

// USDFor converts one stored usage blob into USD. A search fee marked
// search_cost_separate (SEARCH_FEE_INCLUDED=false) is added on top;
// otherwise the fee is already inside cost_usd. Voice legs (STT input,
// TTS replays, dictation polish) are separate bills, always added on top.
func USDFor(usage map[string]any) float64 {
	if len(usage) == 0 {
		return 0
	}
	total := 0.0
	if v, ok := usage["cost_usd"]; ok && v != nil {
		total = toFloat(v)
	} else if v, ok := usage["cost_in_usd_ticks"]; ok && v != nil {
		total = toFloat(v) / ticksPerUSD
	}
	if separate, _ := usage["search_cost_separate"].(bool); separate {
		if v, ok := usage["search_cost_usd"]; ok && v != nil {
			total += toFloat(v)
		}
	}
	for _, key := range []string{"stt_cost_usd", "tts_cost_usd", "polish_cost_usd"} {
		if v, ok := usage[key]; ok && v != nil {
			total += toFloat(v)
		}
	}
	return total
}

// USDForMany sums stored usage JSON blobs.
func USDForMany(blobs []string) float64 {
	var total float64
	for _, b := range blobs {
		var row map[string]any
		if err := json.Unmarshal([]byte(b), &row); err != nil {
			continue
		}
		total += USDFor(row)
	}
	return total
}

// Billed reports whether the blob carries the API's billed total.
func Billed(usage map[string]any) bool {
	if len(usage) == 0 {
		return false
	}
	if v, ok := usage["cost_usd"]; ok && v != nil {
		return true
	}
	v, ok := usage["cost_in_usd_ticks"]
	return ok && v != nil
}

// AllBilled mirrors api_cost_billed?: some rows, all with a billed total.
func AllBilled(blobs []string) bool {
	if len(blobs) == 0 {
		return false
	}
	for _, b := range blobs {
		var row map[string]any
		if err := json.Unmarshal([]byte(b), &row); err != nil || !Billed(row) {
			return false
		}
	}
	return true
}

// FormatUSD mirrors TokenCost.format_usd ("" when nothing to show).
func FormatUSD(usd float64) string {
	if usd <= 0 {
		return ""
	}
	switch {
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	case usd < 1:
		return fmt.Sprintf("$%.3f", usd)
	default:
		return fmt.Sprintf("$%.2f", usd)
	}
}

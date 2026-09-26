package chat

import (
	"math"
	"testing"
)

func usage(pairs ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return m
}

func TestUSDFor(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		want float64
	}{
		{"cost_usd wins",
			usage("input_tokens", 100_000.0, "output_tokens", 100_000.0, "cost_usd", 0.0037756),
			0.0037756},
		{"legacy ticks",
			usage("input_tokens", 100_000.0, "output_tokens", 100_000.0, "cost_in_usd_ticks", 37_756_000.0),
			0.0037756},
		{"tokens only are zero",
			usage("input_tokens", 100_000.0, "cached_tokens", 100_000.0, "output_tokens", 100.0, "reasoning_tokens", 12.0),
			0},
		{"included search fee is not added",
			usage("cost_usd", 0.01, "search_engine", "exa", "search_cost_usd", 0.007),
			0.01},
		{"separate search fee is added",
			usage("cost_usd", 0.01, "search_cost_usd", 0.007, "search_cost_separate", true),
			0.017},
		{"separate fee without base",
			usage("input_tokens", 5.0, "search_cost_usd", 0.007, "search_cost_separate", true),
			0.007},
		{"voice legs are added",
			usage("cost_usd", 0.01, "stt_cost_usd", 0.0001, "tts_cost_usd", 0.00066),
			0.01076},
		{"polish leg is added",
			usage("cost_usd", 0.01, "stt_cost_usd", 0.0001, "polish_cost_usd", 0.00005),
			0.01015},
		{"blank is zero", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := USDFor(tc.row); math.Abs(got-tc.want) > 0.0000001 {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestBilled(t *testing.T) {
	if !Billed(usage("cost_usd", 0.001)) {
		t.Fatal("cost_usd should be billed")
	}
	if !Billed(usage("cost_in_usd_ticks", 1.0)) {
		t.Fatal("ticks should be billed")
	}
	if Billed(usage("input_tokens", 1.0)) {
		t.Fatal("tokens without a total should not be billed")
	}
	if Billed(nil) {
		t.Fatal("nil should not be billed")
	}
}

func TestFormatUSD(t *testing.T) {
	cases := map[float64]string{
		0:      "",
		-1:     "",
		0.0042: "$0.0042",
		0.1234: "$0.123",
		1.234:  "$1.23",
	}
	for in, want := range cases {
		if got := FormatUSD(in); got != want {
			t.Fatalf("FormatUSD(%v) = %q want %q", in, got, want)
		}
	}
}

func TestUSDForMany(t *testing.T) {
	blobs := []string{
		`{"input_tokens":1000,"output_tokens":400,"cost_usd":0.001}`,
		`{"input_tokens":2000,"output_tokens":200,"cost_usd":0.002}`,
		`not json`,
	}
	if got := USDForMany(blobs); math.Abs(got-0.003) > 0.0000001 {
		t.Fatalf("got %v", got)
	}
	if !AllBilled(blobs[:2]) {
		t.Fatal("cost rows should be billed")
	}
	unbilled := []string{`{"input_tokens":1000,"output_tokens":400}`}
	if USDForMany(unbilled) != 0 || AllBilled(unbilled) {
		t.Fatal("tokens-only rows should be unbilled")
	}
}

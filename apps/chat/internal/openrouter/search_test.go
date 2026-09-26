package openrouter

import (
	"context"
	"testing"
)

func TestStreamBodyWithSearch(t *testing.T) {
	var c capture
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	search := &SearchOptions{Engine: "exa", Mode: "auto", MaxResults: 5}
	err := client.StreamChat(context.Background(),
		[]any{map[string]any{"role": "user", "content": "Hi"}}, nil, "high", "kura-9", search, nil,
		func(map[string]any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	plugins, _ := c.payload["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("plugins = %v", c.payload["plugins"])
	}
	p, _ := plugins[0].(map[string]any)
	if p["id"] != "web" || p["engine"] != "exa" || p["mode"] != "auto" || p["max_results"] != 5.0 {
		t.Fatalf("plugin = %v", p)
	}
}

func TestStreamBodyOmitsPluginsWithoutSearch(t *testing.T) {
	var c capture
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	err := client.StreamChat(context.Background(), []any{}, nil, "high", "", nil, nil,
		func(map[string]any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.payload["plugins"]; ok {
		t.Fatalf("payload has plugins: %v", c.payload["plugins"])
	}
}

func TestAnnotationCitations(t *testing.T) {
	ann := func(url, title string) map[string]any {
		inner := map[string]any{
			"url":         url,
			"title":       title,
			"content":     "excerpt",
			"start_index": 1.0,
			"end_index":   2.0,
		}
		return map[string]any{"type": "url_citation", "url_citation": inner}
	}
	deltaChoice := map[string]any{
		"content": "Hi",
		"annotations": []any{
			ann("https://a.test/x", "A"),
			ann("https://b.test/y", "B"),
		},
	}
	delta := map[string]any{
		"choices": []any{
			map[string]any{"delta": deltaChoice},
		},
	}
	got := AnnotationCitations(delta)
	if len(got) != 2 || got[0].URL != "https://a.test/x" || got[0].Title != "A" || got[1].URL != "https://b.test/y" {
		t.Fatalf("delta = %+v", got)
	}
	finalMsg := map[string]any{
		"content": "Hi",
		"annotations": []any{
			ann("https://a.test/x", "A"),
			ann("https://a.test/x", "A dupe"),
			ann("", "empty"),
			map[string]any{"type": "other"},
		},
	}
	final := map[string]any{
		"choices": []any{
			map[string]any{"message": finalMsg},
		},
	}
	got = AnnotationCitations(final)
	if len(got) != 1 || got[0].URL != "https://a.test/x" || got[0].Title != "A" {
		t.Fatalf("final = %+v", got)
	}
	top := map[string]any{
		"message": map[string]any{
			"annotations": []any{ann("https://c.test/", "")},
		},
	}
	got = AnnotationCitations(top)
	if len(got) != 1 || got[0].URL != "https://c.test/" || got[0].Title != "" {
		t.Fatalf("top = %+v", got)
	}
	if AnnotationCitations(map[string]any{}) != nil {
		t.Fatal("empty event should yield nil")
	}
}

func TestValidEffort(t *testing.T) {
	for _, e := range []string{"", "none", "low", "medium", "high", "xhigh", "max"} {
		if !ValidEffort(e) {
			t.Fatalf("effort %q should be valid", e)
		}
	}
	for _, e := range []string{"ultra", "HIGH", " high"} {
		if ValidEffort(e) {
			t.Fatalf("effort %q should be invalid", e)
		}
	}
	if len(Efforts) != 6 {
		t.Fatalf("efforts = %v", Efforts)
	}
}

func TestSearchFeeUSD(t *testing.T) {
	cases := []struct {
		engine, mode string
		max          int
		fee          float64
		ok           bool
	}{
		{"exa", "auto", 5, 0.007, true},
		{"exa", "fast", 5, 0.007, true},
		{"exa", "instant", 5, 0.007, true},
		{"exa", "deep-lite", 5, 0.012, true},
		{"exa", "deep", 5, 0.012, true},
		{"exa", "deep-reasoning", 5, 0.015, true},
		{"exa", "auto", 12, 0.009, true},
		{"", "", 5, 0.007, true},
		{"parallel", "turbo", 5, 0.001, true},
		{"parallel", "fast", 5, 0.001, true},
		{"parallel", "basic", 5, 0.005, true},
		{"parallel", "advanced", 5, 0.005, true},
		{"perplexity", "", 5, 0.005, true},
		{"native", "", 5, 0, false},
		{"firecrawl", "", 5, 0, false},
		{"bogus", "", 5, 0, false},
	}
	for _, tc := range cases {
		fee, ok := SearchFeeUSD(tc.engine, tc.mode, tc.max)
		if ok != tc.ok || fee < tc.fee-1e-9 || fee > tc.fee+1e-9 {
			t.Fatalf("%s/%s x%d = (%v, %v)", tc.engine, tc.mode, tc.max, fee, ok)
		}
	}
}

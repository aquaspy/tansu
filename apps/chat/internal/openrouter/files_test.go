package openrouter

import (
	"context"
	"testing"
)

func TestStreamBodyWithFilePlugin(t *testing.T) {
	var c capture
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	search := &SearchOptions{Engine: "exa", Mode: "auto", MaxResults: 5}
	files := &FileOptions{Engine: "mistral-ocr"}
	err := client.StreamChat(context.Background(),
		[]any{map[string]any{"role": "user", "content": "Hi"}}, nil, "high", "kura-9", search, files,
		func(map[string]any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	plugins, _ := c.payload["plugins"].([]any)
	if len(plugins) != 2 {
		t.Fatalf("plugins = %v", c.payload["plugins"])
	}
	web, _ := plugins[0].(map[string]any)
	parser, _ := plugins[1].(map[string]any)
	if web["id"] != "web" {
		t.Fatalf("plugins[0] = %v", web)
	}
	if parser["id"] != "file-parser" {
		t.Fatalf("plugins[1] = %v", parser)
	}
	pdf, _ := parser["pdf"].(map[string]any)
	if pdf["engine"] != "mistral-ocr" {
		t.Fatalf("pdf = %v", parser["pdf"])
	}
}

func TestStreamBodyOmitsPluginsWithoutFiles(t *testing.T) {
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

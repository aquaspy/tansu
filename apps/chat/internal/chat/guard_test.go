package chat

import (
	"strings"
	"testing"
)

func TestCheckClosingLoop(t *testing.T) {
	prose := "Aqui está a análise completa do caso.\n\n"
	var loop []string
	for range 8 {
		loop = append(loop, "Fim.")
	}
	for range 4 {
		loop = append(loop, "Resposta.")
	}
	text := prose + strings.Join(loop, "\n")
	if got := Check(text); got != "closing_loop" {
		t.Fatalf("check = %q", got)
	}
	truncated := Truncate(text, "closing_loop")
	if !strings.Contains(truncated, "análise completa") {
		t.Fatalf("truncated lost prose: %q", truncated)
	}
	if len(truncated) >= len(text) {
		t.Fatal("truncated is not shorter")
	}
	if strings.Contains(truncated, "Fim.\nFim.\nFim.\nFim") {
		t.Fatalf("loop remains: %q", truncated)
	}
}

func TestCheckURLRepeat(t *testing.T) {
	url := "https://news.example/story"
	text := "Intro.\n" + strings.Repeat(url+"\n", 5)
	if got := Check(text); got != "url_repeat" {
		t.Fatalf("check = %q", got)
	}
	cut := Truncate(text, "url_repeat")
	if strings.Count(cut, url) >= 4 {
		t.Fatalf("flood remains: %q", cut)
	}
}

func TestCheckCleanProse(t *testing.T) {
	text := "Uma resposta normal com um link [fonte](https://ok.example) e fim."
	if got := Check(text); got != "" {
		t.Fatalf("check = %q", got)
	}
}

func TestProtectCodeRoundTrip(t *testing.T) {
	raw := "Use `x` now.\n```\ncode #here\n```\nDone."
	safe, stash := ProtectCode(raw)
	if strings.Contains(safe, "x") && !strings.Contains(safe, "Use") {
		t.Fatalf("safe = %q", safe)
	}
	if got := RestoreCode(safe, stash); got != raw {
		t.Fatalf("got %q want %q", got, raw)
	}
}

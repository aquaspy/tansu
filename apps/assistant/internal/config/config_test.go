package config

import (
	"testing"
)

func TestReasoningEffortDefaultIsXHigh(t *testing.T) {
	t.Setenv("OPENROUTER_REASONING_EFFORT", "")
	if got := Load().OpenRouterReasoningEffort; got != "xhigh" {
		t.Fatalf("default reasoning effort = %q, want xhigh", got)
	}
}

func TestReasoningEffortEnvOverride(t *testing.T) {
	t.Setenv("OPENROUTER_REASONING_EFFORT", "low")
	if got := Load().OpenRouterReasoningEffort; got != "low" {
		t.Fatalf("env override reasoning effort = %q, want low", got)
	}
}

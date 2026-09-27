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

func TestShowModelControlsDefaultsOff(t *testing.T) {
	t.Setenv("ASSISTANT_SHOW_MODEL_CONTROLS", "")
	if Load().ShowModelControls {
		t.Fatal("model controls should be hidden unless ASSISTANT_SHOW_MODEL_CONTROLS is set")
	}
}

func TestShowModelControlsEnv(t *testing.T) {
	t.Setenv("ASSISTANT_SHOW_MODEL_CONTROLS", "true")
	if !Load().ShowModelControls {
		t.Fatal("ASSISTANT_SHOW_MODEL_CONTROLS=true should show the pickers")
	}
	t.Setenv("ASSISTANT_SHOW_MODEL_CONTROLS", "false")
	if Load().ShowModelControls {
		t.Fatal("false should hide the pickers")
	}
}

func TestReasoningEffortEnvOverride(t *testing.T) {
	t.Setenv("OPENROUTER_REASONING_EFFORT", "low")
	if got := Load().OpenRouterReasoningEffort; got != "low" {
		t.Fatalf("env override reasoning effort = %q, want low", got)
	}
}

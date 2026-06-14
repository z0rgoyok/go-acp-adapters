package main

import (
	"os"
	"testing"

	"claude-acp-adapter/internal/app"
)

func TestLoadToolConfigDefaults(t *testing.T) {
	cfg, err := loadToolConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolEvents != app.ToolEventsCompact {
		t.Fatalf("ToolEvents = %q, want compact", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes != 4096 {
		t.Fatalf("ToolInputMaxBytes = %d", cfg.ToolInputMaxBytes)
	}
	if cfg.ToolResultMaxBytes != 8192 {
		t.Fatalf("ToolResultMaxBytes = %d", cfg.ToolResultMaxBytes)
	}
	if cfg.ToolPayloadHardMax != 1048576 {
		t.Fatalf("ToolPayloadHardMax = %d", cfg.ToolPayloadHardMax)
	}
}

func TestLoadToolConfigFromEnv(t *testing.T) {
	t.Setenv("CLAUDE_ACP_TOOL_EVENTS", "full")
	t.Setenv("CLAUDE_ACP_TOOL_INPUT_MAX_BYTES", "2048")
	t.Setenv("CLAUDE_ACP_TOOL_RESULT_MAX_BYTES", "4096")

	cfg, err := loadToolConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolEvents != app.ToolEventsFull {
		t.Fatalf("ToolEvents = %q, want full", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes != 2048 {
		t.Fatalf("ToolInputMaxBytes = %d", cfg.ToolInputMaxBytes)
	}
	if cfg.ToolResultMaxBytes != 4096 {
		t.Fatalf("ToolResultMaxBytes = %d", cfg.ToolResultMaxBytes)
	}
}

func TestLoadToolConfigFromCLI(t *testing.T) {
	cfg, err := loadToolConfig([]string{
		"--tool-events", "off",
		"--tool-input-max-bytes", "100",
		"--tool-result-max-bytes", "200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolEvents != app.ToolEventsOff {
		t.Fatalf("ToolEvents = %q, want off", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes != 100 {
		t.Fatalf("ToolInputMaxBytes = %d", cfg.ToolInputMaxBytes)
	}
	if cfg.ToolResultMaxBytes != 200 {
		t.Fatalf("ToolResultMaxBytes = %d", cfg.ToolResultMaxBytes)
	}
}

func TestLoadToolConfigCLIOverridesEnv(t *testing.T) {
	t.Setenv("CLAUDE_ACP_TOOL_EVENTS", "full")
	t.Setenv("CLAUDE_ACP_TOOL_INPUT_MAX_BYTES", "9999")

	cfg, err := loadToolConfig([]string{
		"--tool-events", "compact",
		"--tool-input-max-bytes", "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolEvents != app.ToolEventsCompact {
		t.Fatalf("ToolEvents = %q, want compact (CLI overrides env)", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes != 100 {
		t.Fatalf("ToolInputMaxBytes = %d, want 100 (CLI overrides env)", cfg.ToolInputMaxBytes)
	}
	if cfg.ToolResultMaxBytes != 8192 {
		t.Fatalf("ToolResultMaxBytes = %d, want 8192 (env not set, use default)", cfg.ToolResultMaxBytes)
	}
}

func TestLoadToolConfigRejectsInvalidEnv(t *testing.T) {
	t.Setenv("CLAUDE_ACP_TOOL_EVENTS", "bogus")
	t.Setenv("CLAUDE_ACP_TOOL_INPUT_MAX_BYTES", "-5")

	_, err := loadToolConfig(nil)
	if err == nil {
		t.Fatal("expected error for invalid env values")
	}
}

func TestLoadToolConfigRejectsInvalidCLI(t *testing.T) {
	t.Setenv("CLAUDE_ACP_TOOL_EVENTS", "compact")

	_, err := loadToolConfig([]string{
		"--tool-events", "bogus",
		"--tool-input-max-bytes", "-1",
		"--tool-result-max-bytes", "abc",
	})
	if err == nil {
		t.Fatal("expected error for invalid CLI values")
	}
}

func TestLoadToolConfigNoEnvFallback(t *testing.T) {
	os.Clearenv()
	cfg, err := loadToolConfig([]string{
		"--tool-events", "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolEvents != app.ToolEventsOff {
		t.Fatalf("ToolEvents = %q, want off", cfg.ToolEvents)
	}
}

func TestLoadToolConfigCLIOverridesInvalidEnv(t *testing.T) {
	t.Setenv("CLAUDE_ACP_TOOL_EVENTS", "bogus")
	t.Setenv("CLAUDE_ACP_TOOL_INPUT_MAX_BYTES", "-5")
	t.Setenv("CLAUDE_ACP_TOOL_RESULT_MAX_BYTES", "not-a-number")

	cfg, err := loadToolConfig([]string{
		"--tool-events", "compact",
		"--tool-input-max-bytes", "100",
		"--tool-result-max-bytes", "200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolEvents != app.ToolEventsCompact {
		t.Fatalf("ToolEvents = %q, want compact", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes != 100 {
		t.Fatalf("ToolInputMaxBytes = %d", cfg.ToolInputMaxBytes)
	}
	if cfg.ToolResultMaxBytes != 200 {
		t.Fatalf("ToolResultMaxBytes = %d", cfg.ToolResultMaxBytes)
	}
}

func TestLoadToolConfigRejectsFlagWithoutValue(t *testing.T) {
	_, err := loadToolConfig([]string{"--tool-events"})
	if err == nil {
		t.Fatal("expected error for --tool-events without value")
	}

	_, err = loadToolConfig([]string{"--tool-input-max-bytes"})
	if err == nil {
		t.Fatal("expected error for --tool-input-max-bytes without value")
	}

	_, err = loadToolConfig([]string{"--tool-result-max-bytes"})
	if err == nil {
		t.Fatal("expected error for --tool-result-max-bytes without value")
	}
}

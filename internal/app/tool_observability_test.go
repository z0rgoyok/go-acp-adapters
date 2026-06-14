package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultToolObservabilityConfig(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	if cfg.ToolEvents != ToolEventsCompact {
		t.Fatalf("ToolEvents = %q", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes != 4096 {
		t.Fatalf("ToolInputMaxBytes = %d", cfg.ToolInputMaxBytes)
	}
	if cfg.ToolResultMaxBytes != 8192 {
		t.Fatalf("ToolResultMaxBytes = %d", cfg.ToolResultMaxBytes)
	}
	if err := ValidateToolObservabilityConfig(cfg); err != nil {
		t.Fatalf("default config validation: %v", err)
	}
}

func TestValidateToolObservabilityConfigRejectsInvalidMode(t *testing.T) {
	err := ValidateToolObservabilityConfig(ToolObservabilityConfig{ToolEvents: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateToolObservabilityConfigRejectsEmptyMode(t *testing.T) {
	err := ValidateToolObservabilityConfig(ToolObservabilityConfig{ToolEvents: ""})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateToolObservabilityConfigRejectsNegativeInputLimit(t *testing.T) {
	err := ValidateToolObservabilityConfig(ToolObservabilityConfig{ToolEvents: ToolEventsCompact, ToolInputMaxBytes: -1})
	if err == nil || !strings.Contains(err.Error(), ">= 0") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateToolObservabilityConfigRejectsNegativeResultLimit(t *testing.T) {
	err := ValidateToolObservabilityConfig(ToolObservabilityConfig{ToolEvents: ToolEventsCompact, ToolResultMaxBytes: -5})
	if err == nil || !strings.Contains(err.Error(), ">= 0") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateToolObservabilityConfigRejectsNonPositiveHardMax(t *testing.T) {
	err := ValidateToolObservabilityConfig(ToolObservabilityConfig{ToolEvents: ToolEventsCompact, ToolPayloadHardMax: 0})
	if err == nil || !strings.Contains(err.Error(), "> 0") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseToolEventMode(t *testing.T) {
	tests := []struct {
		input string
		want  ToolEventMode
	}{
		{"off", ToolEventsOff},
		{"compact", ToolEventsCompact},
		{"full", ToolEventsFull},
		{"OFF", ToolEventsOff},
		{"Compact", ToolEventsCompact},
		{"  full  ", ToolEventsFull},
	}
	for _, tt := range tests {
		got, err := ParseToolEventMode(tt.input)
		if err != nil {
			t.Fatalf("ParseToolEventMode(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("ParseToolEventMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseToolEventModeRejectsUnknown(t *testing.T) {
	_, err := ParseToolEventMode("unknown")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseToolConfigString(t *testing.T) {
	got, err := ParseToolConfigString(json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got = %q", got)
	}
}

func TestParseToolConfigStringRejectsNumber(t *testing.T) {
	_, err := ParseToolConfigString(json.RawMessage(`42`))
	if err == nil {
		t.Fatal("expected error for number input")
	}
}

func TestParseToolConfigInt(t *testing.T) {
	got, err := ParseToolConfigInt(json.RawMessage(`42`))
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Fatalf("got = %d", got)
	}
}

func TestParseToolConfigIntRejectsString(t *testing.T) {
	_, err := ParseToolConfigInt(json.RawMessage(`"hello"`))
	if err == nil {
		t.Fatal("expected error for string input")
	}
}

func TestToolEventsOption(t *testing.T) {
	cfg := SessionConfig{ToolEvents: ToolEventsCompact}
	opt := toolEventsOption(cfg)
	if opt.ID != "toolEvents" || opt.Type != "select" || len(opt.Options) != 3 {
		t.Fatalf("option = %+v", opt)
	}
	var current string
	if err := json.Unmarshal(opt.CurrentValue, &current); err != nil || current != "compact" {
		t.Fatalf("currentValue = %s, want compact", opt.CurrentValue)
	}
}

func TestToolInputMaxBytesOption(t *testing.T) {
	cfg := SessionConfig{ToolInputMaxBytes: 4096}
	opt := toolInputMaxBytesOption(cfg)
	if opt.ID != "toolInputMaxBytes" || opt.Type != "number" {
		t.Fatalf("option = %+v", opt)
	}
	var current int
	if err := json.Unmarshal(opt.CurrentValue, &current); err != nil || current != 4096 {
		t.Fatalf("currentValue = %s, want 4096", opt.CurrentValue)
	}
}

func TestToolResultMaxBytesOption(t *testing.T) {
	cfg := SessionConfig{ToolResultMaxBytes: 8192}
	opt := toolResultMaxBytesOption(cfg)
	if opt.ID != "toolResultMaxBytes" || opt.Type != "number" {
		t.Fatalf("option = %+v", opt)
	}
	var current int
	if err := json.Unmarshal(opt.CurrentValue, &current); err != nil || current != 8192 {
		t.Fatalf("currentValue = %s, want 8192", opt.CurrentValue)
	}
}

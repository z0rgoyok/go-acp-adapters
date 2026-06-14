package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ToolEventMode string

const (
	ToolEventsOff     ToolEventMode = "off"
	ToolEventsCompact ToolEventMode = "compact"
	ToolEventsFull    ToolEventMode = "full"
)

type ToolObservabilityConfig struct {
	ToolEvents         ToolEventMode
	ToolInputMaxBytes  int
	ToolResultMaxBytes int
	ToolPayloadHardMax int
}

func DefaultToolObservabilityConfig() ToolObservabilityConfig {
	return ToolObservabilityConfig{
		ToolEvents:         ToolEventsCompact,
		ToolInputMaxBytes:  4096,
		ToolResultMaxBytes: 8192,
		ToolPayloadHardMax: 1048576,
	}
}

func ValidateToolObservabilityConfig(cfg ToolObservabilityConfig) error {
	switch cfg.ToolEvents {
	case ToolEventsOff, ToolEventsCompact, ToolEventsFull:
	case "":
		return fmt.Errorf("toolEvents mode is required")
	default:
		return fmt.Errorf("unsupported toolEvents mode: %q", cfg.ToolEvents)
	}
	if cfg.ToolInputMaxBytes < 0 {
		return fmt.Errorf("toolInputMaxBytes must be >= 0")
	}
	if cfg.ToolResultMaxBytes < 0 {
		return fmt.Errorf("toolResultMaxBytes must be >= 0")
	}
	if cfg.ToolPayloadHardMax <= 0 {
		return fmt.Errorf("toolPayloadHardMax must be > 0")
	}
	return nil
}

func ParseToolEventMode(value string) (ToolEventMode, error) {
	v := strings.TrimSpace(strings.ToLower(value))
	switch v {
	case "off":
		return ToolEventsOff, nil
	case "compact":
		return ToolEventsCompact, nil
	case "full":
		return ToolEventsFull, nil
	default:
		return "", fmt.Errorf("unsupported toolEvents value: %q", value)
	}
}

func ParseToolConfigString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("expected string value")
	}
	return strings.TrimSpace(s), nil
}

func ParseToolConfigInt(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("expected integer value")
	}
	return n, nil
}

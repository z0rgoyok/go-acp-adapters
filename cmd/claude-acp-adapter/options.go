package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"claude-acp-adapter/internal/app"
)

func loadToolConfig(args []string) (app.ToolObservabilityConfig, error) {
	cfg := app.DefaultToolObservabilityConfig()

	cliSet := detectCLIToolFlags(args)

	if !cliSet["toolEvents"] {
		if v := os.Getenv("CLAUDE_ACP_TOOL_EVENTS"); v != "" {
			mode, err := app.ParseToolEventMode(v)
			if err != nil {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid CLAUDE_ACP_TOOL_EVENTS=%q: %w", v, err)
			}
			cfg.ToolEvents = mode
		}
	}
	if !cliSet["toolInputMaxBytes"] {
		if v := os.Getenv("CLAUDE_ACP_TOOL_INPUT_MAX_BYTES"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid CLAUDE_ACP_TOOL_INPUT_MAX_BYTES=%q: must be an integer", v)
			}
			if n < 0 {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid CLAUDE_ACP_TOOL_INPUT_MAX_BYTES=%q: must be >= 0", v)
			}
			cfg.ToolInputMaxBytes = n
		}
	}
	if !cliSet["toolResultMaxBytes"] {
		if v := os.Getenv("CLAUDE_ACP_TOOL_RESULT_MAX_BYTES"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid CLAUDE_ACP_TOOL_RESULT_MAX_BYTES=%q: must be an integer", v)
			}
			if n < 0 {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid CLAUDE_ACP_TOOL_RESULT_MAX_BYTES=%q: must be >= 0", v)
			}
			cfg.ToolResultMaxBytes = n
		}
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tool-events":
			if i+1 >= len(args) {
				return app.ToolObservabilityConfig{}, fmt.Errorf("--tool-events requires a value (off|compact|full)")
			}
			i++
			mode, err := app.ParseToolEventMode(strings.TrimSpace(args[i]))
			if err != nil {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid --tool-events=%q: %w", args[i], err)
			}
			cfg.ToolEvents = mode
		case "--tool-input-max-bytes":
			if i+1 >= len(args) {
				return app.ToolObservabilityConfig{}, fmt.Errorf("--tool-input-max-bytes requires a value")
			}
			i++
			n, err := strconv.Atoi(strings.TrimSpace(args[i]))
			if err != nil {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid --tool-input-max-bytes=%q: must be an integer", args[i])
			}
			if n < 0 {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid --tool-input-max-bytes=%q: must be >= 0", args[i])
			}
			cfg.ToolInputMaxBytes = n
		case "--tool-result-max-bytes":
			if i+1 >= len(args) {
				return app.ToolObservabilityConfig{}, fmt.Errorf("--tool-result-max-bytes requires a value")
			}
			i++
			n, err := strconv.Atoi(strings.TrimSpace(args[i]))
			if err != nil {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid --tool-result-max-bytes=%q: must be an integer", args[i])
			}
			if n < 0 {
				return app.ToolObservabilityConfig{}, fmt.Errorf("invalid --tool-result-max-bytes=%q: must be >= 0", args[i])
			}
			cfg.ToolResultMaxBytes = n
		}
	}

	return cfg, nil
}

func detectCLIToolFlags(args []string) map[string]bool {
	set := map[string]bool{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tool-events":
			set["toolEvents"] = true
			if i+1 < len(args) {
				i++
			}
		case "--tool-input-max-bytes":
			set["toolInputMaxBytes"] = true
			if i+1 < len(args) {
				i++
			}
		case "--tool-result-max-bytes":
			set["toolResultMaxBytes"] = true
			if i+1 < len(args) {
				i++
			}
		}
	}
	return set
}

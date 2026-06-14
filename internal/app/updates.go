package app

import (
	"bytes"
	"encoding/json"
	"strings"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func updateFromTranscriptEvent(event claude.TranscriptEvent, cfg SessionConfig) (acp.SessionUpdate, bool) {
	switch e := event.(type) {
	case claude.AssistantTextEvent:
		if strings.TrimSpace(e.Text) == "" && e.StopReason == "" {
			return acp.SessionUpdate{}, false
		}
		content, _ := json.Marshal(acp.ContentBlock{Type: "text", Text: e.Text})
		return acp.SessionUpdate{
			SessionUpdate: "agent_message_chunk",
			MessageID:     e.MessageID,
			Content:       content,
		}, true

	case claude.AssistantToolUseEvent:
		if cfg.ToolEvents == ToolEventsOff {
			return acp.SessionUpdate{}, false
		}
		input := e.Input
		originalBytes := len(input)
		truncated := false
		if cfg.ToolEvents == ToolEventsCompact && cfg.ToolInputMaxBytes > 0 && originalBytes > cfg.ToolInputMaxBytes {
			input = truncateJSONPreview(input, cfg.ToolInputMaxBytes)
			truncated = true
		}
		if cfg.ToolPayloadHardMax > 0 && len(input) > cfg.ToolPayloadHardMax {
			input = truncateJSONPreview(input, cfg.ToolPayloadHardMax)
			truncated = true
		}
		meta := make(map[string]any)
		if truncated {
			meta["truncated"] = true
			meta["originalBytes"] = originalBytes
		}
		return acp.SessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    e.ToolUseID,
			Title:         buildToolTitle(e),
			Kind:          toolKind(e.Name),
			Status:        acp.ToolCallStatusPending,
			RawInput:      input,
			Meta:          meta,
		}, true

	case claude.ToolResultEvent:
		if cfg.ToolEvents == ToolEventsOff {
			return acp.SessionUpdate{}, false
		}
		var contentStr string
		var rawOutput json.RawMessage
		originalBytes := 0
		truncated := false

		if cfg.ToolEvents == ToolEventsCompact {
			contentStr = resultContentString(e.Content)
			originalBytes = len(contentStr)
			if cfg.ToolResultMaxBytes > 0 && originalBytes > cfg.ToolResultMaxBytes {
				contentStr = contentStr[:cfg.ToolResultMaxBytes]
				truncated = true
			}
		} else {
			rawOutput = e.Content
			contentStr = resultContentString(e.Content)
			originalBytes = len(e.Content)
		}

		if cfg.ToolPayloadHardMax > 0 && len(contentStr) > cfg.ToolPayloadHardMax {
			contentStr = contentStr[:cfg.ToolPayloadHardMax]
			truncated = true
		}

		if cfg.ToolEvents == ToolEventsFull && cfg.ToolPayloadHardMax > 0 && len(rawOutput) > cfg.ToolPayloadHardMax {
			rawOutput = truncateJSONPreview(rawOutput, cfg.ToolPayloadHardMax)
			truncated = true
		}

		content := buildToolCallContentArray(contentStr)

		status := acp.ToolCallStatusCompleted
		if e.IsError {
			status = acp.ToolCallStatusFailed
		}
		meta := make(map[string]any)
		if e.IsError {
			meta["isError"] = true
		}
		if truncated {
			meta["truncated"] = true
			meta["originalBytes"] = originalBytes
		}
		update := acp.SessionUpdate{
			SessionUpdate: "tool_call_update",
			ToolCallID:    e.ToolUseID,
			Status:        status,
			Content:       content,
			Meta:          meta,
		}
		if rawOutput != nil {
			update.RawOutput = rawOutput
		}
		return update, true

	default:
		return acp.SessionUpdate{}, false
	}
}

func toolKind(name string) acp.ToolKind {
	switch name {
	case "Read":
		return acp.ToolKindRead
	case "Write", "Edit", "MultiEdit":
		return acp.ToolKindEdit
	case "Bash":
		return acp.ToolKindExecute
	case "Glob", "Grep", "Search":
		return acp.ToolKindSearch
	case "Delete", "FileDelete":
		return acp.ToolKindDelete
	case "Move", "Rename":
		return acp.ToolKindMove
	case "Think":
		return acp.ToolKindThink
	case "WebFetch", "Fetch":
		return acp.ToolKindFetch
	case "SwitchMode":
		return acp.ToolKindSwitchMode
	default:
		return ""
	}
}

func buildToolCallContentArray(text string) json.RawMessage {
	result, _ := json.Marshal([]acp.ToolCallContent{
		{
			Type: "content",
			Content: acp.ContentBlock{
				Type: "text",
				Text: text,
			},
		},
	})
	return result
}

func buildToolTitle(event claude.AssistantToolUseEvent) string {
	if event.Name == "" {
		return "tool"
	}
	var args map[string]any
	if err := json.Unmarshal(event.Input, &args); err == nil {
		for _, key := range []string{"file_path", "path", "command", "url", "name"} {
			if v, ok := args[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return event.Name + " " + s
				}
			}
		}
	}
	return event.Name
}

func resultContentString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var texts []string
		for _, b := range blocks {
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err == nil {
		return compact.String()
	}
	return string(raw)
}

func truncateJSONPreview(raw json.RawMessage, maxBytes int) json.RawMessage {
	if len(raw) <= maxBytes {
		return raw
	}
	preview := string(raw[:maxBytes]) + "..."
	result, _ := json.Marshal(preview)
	return result
}

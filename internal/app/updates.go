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
		return acp.SessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    e.ToolUseID,
			Title:         buildToolTitle(e),
			Kind:          e.Name,
			Status:        "started",
			Input:         input,
			Truncated:     truncated,
			OriginalBytes: originalBytes,
		}, true

	case claude.ToolResultEvent:
		if cfg.ToolEvents == ToolEventsOff {
			return acp.SessionUpdate{}, false
		}
		var content json.RawMessage
		originalBytes := 0
		truncated := false

		if cfg.ToolEvents == ToolEventsCompact {
			contentStr := resultContentString(e.Content)
			originalBytes = len(contentStr)
			if cfg.ToolResultMaxBytes > 0 && originalBytes > cfg.ToolResultMaxBytes {
				contentStr = contentStr[:cfg.ToolResultMaxBytes]
				truncated = true
			}
			content, _ = json.Marshal(contentStr)
		} else {
			content = e.Content
			originalBytes = len(content)
		}

		if cfg.ToolPayloadHardMax > 0 && len(content) > cfg.ToolPayloadHardMax {
			content = truncateJSONPreview(content, cfg.ToolPayloadHardMax)
			truncated = true
		}

		status := "completed"
		if e.IsError {
			status = "failed"
		}
		return acp.SessionUpdate{
			SessionUpdate: "tool_call_update",
			ToolCallID:    e.ToolUseID,
			Status:        status,
			IsError:       &e.IsError,
			Content:       content,
			Truncated:     truncated,
			OriginalBytes: originalBytes,
		}, true

	default:
		return acp.SessionUpdate{}, false
	}
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

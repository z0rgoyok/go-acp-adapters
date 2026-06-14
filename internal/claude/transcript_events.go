package claude

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"
)

func parseTranscriptEvents(line []byte, byteOffset int64) []TranscriptEvent {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return []TranscriptEvent{TranscriptDiagnosticEvent{ByteOffset: byteOffset, EventType: "invalid_json", Message: err.Error()}}
	}

	eventType := rawString(raw["type"])
	timestamp := rawTimestamp(raw["timestamp"])
	switch eventType {
	case "assistant":
		return parseAssistantEvents(raw, byteOffset, timestamp)
	case "user":
		return parseUserEvents(raw, byteOffset, timestamp)
	case "ai-title":
		return parseTitleEvents(raw, byteOffset, timestamp)
	case "system", "attachment", "last-prompt", "mode", "permission-mode", "file-history-snapshot", "queue-operation":
		return nil
	default:
		keys := slices.Collect(maps.Keys(raw))
		slices.Sort(keys)
		return []TranscriptEvent{UnknownTranscriptEvent{ByteOffset: byteOffset, EventType: eventType, Keys: keys}}
	}
}

func parseAssistantEvents(raw map[string]json.RawMessage, byteOffset int64, timestamp time.Time) []TranscriptEvent {
	var message struct {
		ID         string            `json:"id"`
		Role       string            `json:"role"`
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
		Usage      json.RawMessage   `json:"usage"`
	}
	if err := json.Unmarshal(raw["message"], &message); err != nil {
		return []TranscriptEvent{TranscriptDiagnosticEvent{ByteOffset: byteOffset, EventType: "assistant", Message: err.Error()}}
	}
	if message.Role != "assistant" {
		return nil
	}
	messageID := message.ID
	if messageID == "" {
		messageID = rawString(raw["uuid"])
	}

	var events []TranscriptEvent
	var texts []string
	textEmitted := false
	flushText := func(stopReason string) {
		if len(texts) == 0 && stopReason == "" {
			return
		}
		events = append(events, AssistantTextEvent{ByteOffset: byteOffset, Timestamp: timestamp, MessageID: messageID, Text: strings.Join(texts, "\n"), StopReason: stopReason})
		texts = nil
		textEmitted = true
	}
	for _, blockRaw := range message.Content {
		var block struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(blockRaw, &block); err != nil {
			events = append(events, TranscriptDiagnosticEvent{ByteOffset: byteOffset, EventType: "assistant.content", Message: err.Error()})
			continue
		}
		switch block.Type {
		case "text":
			if block.Text != "" {
				texts = append(texts, block.Text)
			}
		case "tool_use":
			flushText("")
			events = append(events, AssistantToolUseEvent{ByteOffset: byteOffset, Timestamp: timestamp, MessageID: messageID, ToolUseID: block.ID, Name: block.Name, Input: cloneRaw(block.Input)})
		}
	}
	flushText(message.StopReason)
	if message.StopReason != "" && !textEmitted {
		events = append(events, AssistantTextEvent{ByteOffset: byteOffset, Timestamp: timestamp, MessageID: messageID, StopReason: message.StopReason})
	}
	if len(message.Usage) > 0 && string(message.Usage) != "null" {
		events = append(events, UsageEvent{ByteOffset: byteOffset, Timestamp: timestamp, Usage: cloneRaw(message.Usage)})
	}
	return events
}

func parseUserEvents(raw map[string]json.RawMessage, byteOffset int64, timestamp time.Time) []TranscriptEvent {
	var message struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw["message"], &message); err != nil {
		return []TranscriptEvent{TranscriptDiagnosticEvent{ByteOffset: byteOffset, EventType: "user", Message: err.Error()}}
	}
	var events []TranscriptEvent
	for _, blockRaw := range message.Content {
		var block struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		}
		if err := json.Unmarshal(blockRaw, &block); err != nil {
			events = append(events, TranscriptDiagnosticEvent{ByteOffset: byteOffset, EventType: "user.content", Message: err.Error()})
			continue
		}
		if block.Type == "tool_result" {
			events = append(events, ToolResultEvent{ByteOffset: byteOffset, Timestamp: timestamp, ToolUseID: block.ToolUseID, Content: cloneRaw(block.Content), IsError: block.IsError})
		}
	}
	return events
}

func parseTitleEvents(raw map[string]json.RawMessage, byteOffset int64, timestamp time.Time) []TranscriptEvent {
	title := rawString(raw["title"])
	if title == "" {
		title = rawString(raw["text"])
	}
	if title == "" {
		return nil
	}
	return []TranscriptEvent{SessionTitleEvent{ByteOffset: byteOffset, Timestamp: timestamp, Title: title}}
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func rawTimestamp(raw json.RawMessage) time.Time {
	value := rawString(raw)
	if value == "" {
		return time.Time{}
	}
	timestamp, _ := time.Parse(time.RFC3339Nano, value)
	return timestamp
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

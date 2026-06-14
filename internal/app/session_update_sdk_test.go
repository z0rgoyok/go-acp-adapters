package app

import (
	"encoding/json"
	"strings"
	"testing"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"

	sdk "github.com/coder/acp-go-sdk"
)

func TestSDKDecodeAgentMessageChunk(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	scfg := SessionConfig{ToolEvents: cfg.ToolEvents}

	event := claude.AssistantTextEvent{MessageID: "msg-1", Text: "hello"}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}

	var sdkUpdate sdk.SessionUpdate
	if err := json.Unmarshal(data, &sdkUpdate); err != nil {
		t.Fatalf("SDK decode failed: %v\npayload: %s", err, data)
	}
	if sdkUpdate.AgentMessageChunk == nil {
		t.Fatal("expected AgentMessageChunk variant")
	}
	if sdkUpdate.AgentMessageChunk.Content.Text == nil || sdkUpdate.AgentMessageChunk.Content.Text.Text != "hello" {
		t.Fatalf("text = %+v", sdkUpdate.AgentMessageChunk.Content.Text)
	}
}

func TestSDKDecodeToolCall(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	scfg := SessionConfig{ToolEvents: cfg.ToolEvents}

	event := claude.AssistantToolUseEvent{
		ToolUseID: "tool-1",
		Name:      "Read",
		Input:     json.RawMessage(`{"file_path":"/tmp/a.txt"}`),
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}

	var sdkUpdate sdk.SessionUpdate
	if err := json.Unmarshal(data, &sdkUpdate); err != nil {
		t.Fatalf("SDK decode failed: %v\npayload: %s", err, data)
	}
	if sdkUpdate.ToolCall == nil {
		t.Fatal("expected ToolCall variant")
	}
	if sdkUpdate.ToolCall.ToolCallId != "tool-1" {
		t.Fatalf("toolCallId = %q", sdkUpdate.ToolCall.ToolCallId)
	}
	if sdkUpdate.ToolCall.Kind != sdk.ToolKindRead {
		t.Fatalf("kind = %q", sdkUpdate.ToolCall.Kind)
	}
	if sdkUpdate.ToolCall.Status != sdk.ToolCallStatusPending {
		t.Fatalf("status = %q", sdkUpdate.ToolCall.Status)
	}
}

func TestSDKDecodeToolCallUpdate(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	scfg := SessionConfig{ToolEvents: cfg.ToolEvents}

	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`"file contents"`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}

	var sdkUpdate sdk.SessionUpdate
	if err := json.Unmarshal(data, &sdkUpdate); err != nil {
		t.Fatalf("SDK decode failed: %v\npayload: %s", err, data)
	}
	if sdkUpdate.ToolCallUpdate == nil {
		t.Fatal("expected ToolCallUpdate variant")
	}
	if sdkUpdate.ToolCallUpdate.Status == nil || *sdkUpdate.ToolCallUpdate.Status != sdk.ToolCallStatusCompleted {
		t.Fatalf("status = %v", sdkUpdate.ToolCallUpdate.Status)
	}
	if len(sdkUpdate.ToolCallUpdate.Content) != 1 {
		t.Fatalf("content length = %d", len(sdkUpdate.ToolCallUpdate.Content))
	}
}

func TestSDKDecodeToolCallUpdateError(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	scfg := SessionConfig{ToolEvents: cfg.ToolEvents}

	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`"error"`),
		IsError:   true,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}

	var sdkUpdate sdk.SessionUpdate
	if err := json.Unmarshal(data, &sdkUpdate); err != nil {
		t.Fatalf("SDK decode failed: %v\npayload: %s", err, data)
	}
	if sdkUpdate.ToolCallUpdate == nil {
		t.Fatal("expected ToolCallUpdate variant")
	}
	if sdkUpdate.ToolCallUpdate.Status == nil || *sdkUpdate.ToolCallUpdate.Status != sdk.ToolCallStatusFailed {
		t.Fatalf("status = %v", sdkUpdate.ToolCallUpdate.Status)
	}
}

func TestSDKDecodeToolCallKindMappings(t *testing.T) {
	tests := []struct {
		claudeName string
		wantKind   sdk.ToolKind
	}{
		{"Read", sdk.ToolKindRead},
		{"Edit", sdk.ToolKindEdit},
		{"Write", sdk.ToolKindEdit},
		{"MultiEdit", sdk.ToolKindEdit},
		{"Bash", sdk.ToolKindExecute},
		{"Glob", sdk.ToolKindSearch},
		{"Grep", sdk.ToolKindSearch},
		{"Think", sdk.ToolKindThink},
		{"WebFetch", sdk.ToolKindFetch},
		{"Fetch", sdk.ToolKindFetch},
		{"SwitchMode", sdk.ToolKindSwitchMode},
		{"Delete", sdk.ToolKindDelete},
		{"FileDelete", sdk.ToolKindDelete},
		{"Move", sdk.ToolKindMove},
		{"Rename", sdk.ToolKindMove},
		{"UnknownTool", ""},
	}

	scfg := SessionConfig{ToolEvents: ToolEventsCompact, ToolInputMaxBytes: 4096}
	for _, tt := range tests {
		t.Run(tt.claudeName, func(t *testing.T) {
			event := claude.AssistantToolUseEvent{
				ToolUseID: "t1",
				Name:      tt.claudeName,
				Input:     json.RawMessage(`{}`),
			}
			update, ok := updateFromTranscriptEvent(event, scfg)
			if !ok {
				t.Fatal("expected ok")
			}
			data, _ := json.Marshal(update)
			var sdkUpdate sdk.SessionUpdate
			if err := json.Unmarshal(data, &sdkUpdate); err != nil {
				t.Fatalf("SDK decode failed for %s: %v", tt.claudeName, err)
			}
			if sdkUpdate.ToolCall == nil {
				t.Fatal("expected ToolCall variant")
			}
			if sdkUpdate.ToolCall.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", sdkUpdate.ToolCall.Kind, tt.wantKind)
			}
		})
	}
}

func TestSDKDecodeToolCallFullMode(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsFull}

	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`[{"type":"text","text":"hello"},{"type":"text","text":"world"}]`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}

	var sdkUpdate sdk.SessionUpdate
	if err := json.Unmarshal(data, &sdkUpdate); err != nil {
		t.Fatalf("SDK decode failed: %v\npayload: %s", err, data)
	}
	if sdkUpdate.ToolCallUpdate == nil {
		t.Fatal("expected ToolCallUpdate variant")
	}
	if len(sdkUpdate.ToolCallUpdate.Content) != 1 {
		t.Fatalf("content length = %d", len(sdkUpdate.ToolCallUpdate.Content))
	}
}

func TestSDKDecodeInterleavingStream(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsCompact, ToolInputMaxBytes: 4096, ToolResultMaxBytes: 8192}

	events := []claude.TranscriptEvent{
		claude.AssistantTextEvent{MessageID: "msg-1", Text: "one"},
		claude.AssistantToolUseEvent{ToolUseID: "tool-1", Name: "Read", Input: json.RawMessage(`{"file_path":"/tmp/a.txt"}`)},
		claude.ToolResultEvent{ToolUseID: "tool-1", Content: json.RawMessage(`"content"`), IsError: false},
		claude.AssistantTextEvent{MessageID: "msg-1", Text: "two"},
	}

	for i, event := range events {
		update, ok := updateFromTranscriptEvent(event, scfg)
		if !ok {
			t.Fatalf("event[%d]: expected ok", i)
		}
		data, err := json.Marshal(update)
		if err != nil {
			t.Fatalf("event[%d] marshal: %v", i, err)
		}
		var sdkUpdate sdk.SessionUpdate
		if err := json.Unmarshal(data, &sdkUpdate); err != nil {
			t.Fatalf("event[%d] SDK decode failed: %v\npayload: %s", i, err, data)
		}
	}
}

func TestSDKRejectsOldWireShape(t *testing.T) {
	// tool_call with status=started is accepted by SDK (ToolCallStatus is a string
	// alias, unknown values pass through). The actual breakage was tool_call_update
	// with string content, which fails with "invalid variant payload".
	oldToolCallUpdate := `{
		"sessionUpdate": "tool_call_update",
		"toolCallId": "tool-1",
		"status": "completed",
		"content": "file contents"
	}`
	var sdkUpdate sdk.SessionUpdate
	if err := json.Unmarshal([]byte(oldToolCallUpdate), &sdkUpdate); err == nil {
		t.Fatal("expected SDK to reject old tool_call_update with string content")
	}
}

func TestToolKind(t *testing.T) {
	tests := []struct {
		name string
		want acp.ToolKind
	}{
		{"Read", acp.ToolKindRead},
		{"Edit", acp.ToolKindEdit},
		{"Write", acp.ToolKindEdit},
		{"MultiEdit", acp.ToolKindEdit},
		{"Bash", acp.ToolKindExecute},
		{"Glob", acp.ToolKindSearch},
		{"Grep", acp.ToolKindSearch},
		{"Search", acp.ToolKindSearch},
		{"Think", acp.ToolKindThink},
		{"WebFetch", acp.ToolKindFetch},
		{"Fetch", acp.ToolKindFetch},
		{"SwitchMode", acp.ToolKindSwitchMode},
		{"Delete", acp.ToolKindDelete},
		{"FileDelete", acp.ToolKindDelete},
		{"Move", acp.ToolKindMove},
		{"Rename", acp.ToolKindMove},
		{"CustomTool", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := toolKind(tt.name)
		if got != tt.want {
			t.Fatalf("toolKind(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestBuildToolCallContentArray(t *testing.T) {
	data := buildToolCallContentArray("hello world")
	var blocks []struct {
		Type    string `json:"type"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len = %d", len(blocks))
	}
	if blocks[0].Type != "content" {
		t.Fatalf("type = %q", blocks[0].Type)
	}
	if blocks[0].Content.Type != "text" || blocks[0].Content.Text != "hello world" {
		t.Fatalf("content = %+v", blocks[0].Content)
	}
}

func TestBuildToolCallContentArrayEmpty(t *testing.T) {
	data := buildToolCallContentArray("")
	var blocks []acp.ToolCallContent
	if err := json.Unmarshal(data, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("len = %d", len(blocks))
	}
	if blocks[0].Content.Text != "" {
		t.Fatalf("text = %q", blocks[0].Content.Text)
	}
}

func TestMetaIsEmptyForNormalResults(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsCompact}
	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`"ok"`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	// Meta should have no entries for non-error, non-truncated results.
	if len(update.Meta) != 0 {
		metaJSON, _ := json.Marshal(update.Meta)
		t.Fatalf("meta should be empty for non-error, non-truncated, got %s", metaJSON)
	}
}

func TestNoKindForUnknownTool(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsCompact}
	event := claude.AssistantToolUseEvent{
		ToolUseID: "t1",
		Name:      "SomeRandomTool",
		Input:     json.RawMessage(`{}`),
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	if update.Kind != "" {
		t.Fatalf("kind should be empty for unknown tools, got %q", update.Kind)
	}
	data, _ := json.Marshal(update)
	if strings.Contains(string(data), `"kind"`) {
		t.Fatalf("kind should be omitted from JSON for unknown tools: %s", data)
	}
}

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func TestUpdateFromTranscriptEvent_AssistantText(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	scfg := SessionConfig{ToolEvents: cfg.ToolEvents}

	event := claude.AssistantTextEvent{MessageID: "msg-1", Text: "hello"}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	if update.SessionUpdate != "agent_message_chunk" {
		t.Fatalf("type = %q", update.SessionUpdate)
	}
	if update.MessageID != "msg-1" {
		t.Fatalf("messageId = %q", update.MessageID)
	}
	var content acp.ContentBlock
	if err := json.Unmarshal(update.Content, &content); err != nil {
		t.Fatal(err)
	}
	if content.Text != "hello" {
		t.Fatalf("text = %q", content.Text)
	}
	if update.ToolCallID != "" || update.Kind != "" {
		t.Fatal("tool fields should be empty for text")
	}
}

func TestUpdateFromTranscriptEvent_ToolCall(t *testing.T) {
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
	if update.SessionUpdate != "tool_call" {
		t.Fatalf("type = %q", update.SessionUpdate)
	}
	if update.ToolCallID != "tool-1" {
		t.Fatalf("toolCallId = %q", update.ToolCallID)
	}
	if update.Kind != acp.ToolKindRead {
		t.Fatalf("kind = %q", update.Kind)
	}
	if update.Status != acp.ToolCallStatusPending {
		t.Fatalf("status = %q", update.Status)
	}
	if update.Title != "Read /tmp/a.txt" {
		t.Fatalf("title = %q", update.Title)
	}
	if len(update.Meta) != 0 {
		t.Fatal("expected no meta for non-truncated small input")
	}
	var rawInput map[string]any
	if err := json.Unmarshal(update.RawInput, &rawInput); err != nil {
		t.Fatal(err)
	}
	if rawInput["file_path"] != "/tmp/a.txt" {
		t.Fatalf("rawInput = %+v", rawInput)
	}
}

func TestUpdateFromTranscriptEvent_ToolCallCompactTruncation(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents:        ToolEventsCompact,
		ToolInputMaxBytes: 20,
	}
	longInput := `{"file_path":"` + strings.Repeat("x", 100) + `"}`
	event := claude.AssistantToolUseEvent{
		ToolUseID: "tool-1",
		Name:      "Read",
		Input:     json.RawMessage(longInput),
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	meta := update.Meta
	if meta == nil || meta["truncated"] != true {
		t.Fatal("expected truncated=true in meta")
	}
	if meta["originalBytes"] != len(longInput) {
		t.Fatalf("originalBytes = %d, want %d", meta["originalBytes"], len(longInput))
	}
}

func TestUpdateFromTranscriptEvent_ToolCallHardLimit(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents:         ToolEventsFull,
		ToolPayloadHardMax: 20,
	}
	longInput := `{"file_path":"` + strings.Repeat("x", 100) + `"}`
	event := claude.AssistantToolUseEvent{
		ToolUseID: "tool-1",
		Name:      "Read",
		Input:     json.RawMessage(longInput),
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	meta := update.Meta
	if meta == nil || meta["truncated"] != true {
		t.Fatal("expected truncated=true in meta")
	}
	if meta["originalBytes"] != len(longInput) {
		t.Fatalf("originalBytes = %d, want %d", meta["originalBytes"], len(longInput))
	}
}

func TestUpdateFromTranscriptEvent_ToolResultCompleted(t *testing.T) {
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
	if update.SessionUpdate != "tool_call_update" {
		t.Fatalf("type = %q", update.SessionUpdate)
	}
	if update.Status != acp.ToolCallStatusCompleted {
		t.Fatalf("status = %q", update.Status)
	}
	if update.Meta != nil && update.Meta["isError"] != nil {
		t.Fatal("isError should be absent for non-error result")
	}
	if update.ToolCallID != "tool-1" {
		t.Fatalf("toolCallId = %q", update.ToolCallID)
	}
	var contentBlocks []acp.ToolCallContent
	if err := json.Unmarshal(update.Content, &contentBlocks); err != nil {
		t.Fatalf("content should be ToolCallContent array: %v", err)
	}
	if len(contentBlocks) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contentBlocks))
	}
	if contentBlocks[0].Type != "content" {
		t.Fatalf("block type = %q", contentBlocks[0].Type)
	}
	if contentBlocks[0].Content.Type != "text" || contentBlocks[0].Content.Text != "file contents" {
		t.Fatalf("block content = %+v", contentBlocks[0].Content)
	}
}

func TestUpdateFromTranscriptEvent_ToolResultFailed(t *testing.T) {
	cfg := DefaultToolObservabilityConfig()
	scfg := SessionConfig{ToolEvents: cfg.ToolEvents}

	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`"error message"`),
		IsError:   true,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	if update.Status != acp.ToolCallStatusFailed {
		t.Fatalf("status = %q, want failed", update.Status)
	}
	if update.Meta == nil || update.Meta["isError"] != true {
		t.Fatal("isError should be true in meta")
	}
}

func TestUpdateFromTranscriptEvent_ToolResultCompactTruncation(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents:         ToolEventsCompact,
		ToolResultMaxBytes: 10,
	}
	longContent := strings.Repeat("x", 100)
	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`"` + longContent + `"`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	meta := update.Meta
	if meta == nil || meta["truncated"] != true {
		t.Fatal("expected truncated=true in meta")
	}
	if meta["originalBytes"] != 100 {
		t.Fatalf("originalBytes = %d, want 100", meta["originalBytes"])
	}
}

func TestUpdateFromTranscriptEvent_ToolResultFullPreservesJSON(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents: ToolEventsFull,
	}

	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`[{"type":"text","text":"hello"},{"type":"text","text":"world"}]`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	var tccs []acp.ToolCallContent
	if err := json.Unmarshal(update.Content, &tccs); err != nil {
		t.Fatalf("content should be ToolCallContent array: %v", err)
	}
	if len(tccs) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(tccs))
	}
	if update.RawOutput == nil {
		t.Fatal("rawOutput should be set in full mode")
	}
	var blocks []map[string]any
	if err := json.Unmarshal(update.RawOutput, &blocks); err != nil {
		t.Fatalf("rawOutput should be JSON array: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks in rawOutput, got %d", len(blocks))
	}
}

func TestUpdateFromTranscriptEvent_ToolResultFullPreservesObject(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents: ToolEventsFull,
	}

	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`{"result":"success","data":{"key":"value"}}`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}

	var tccs []acp.ToolCallContent
	if err := json.Unmarshal(update.Content, &tccs); err != nil {
		t.Fatalf("content should be ToolCallContent array: %v", err)
	}
	if len(tccs) != 1 || tccs[0].Content.Text != `{"result":"success","data":{"key":"value"}}` {
		t.Fatalf("content = %+v", tccs)
	}
	if update.RawOutput == nil {
		t.Fatal("rawOutput should be set in full mode")
	}
	var obj map[string]any
	if err := json.Unmarshal(update.RawOutput, &obj); err != nil {
		t.Fatalf("rawOutput should be JSON object: %v", err)
	}
	if obj["result"] != "success" {
		t.Fatalf("result = %v", obj["result"])
	}
}

func TestUpdateFromTranscriptEvent_ToolResultFullHardLimit(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents:         ToolEventsFull,
		ToolPayloadHardMax: 50,
	}

	longContent := `"` + strings.Repeat("x", 100) + `"`
	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(longContent),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	meta := update.Meta
	if meta == nil || meta["truncated"] != true {
		t.Fatal("expected truncated=true in meta")
	}
	if meta["originalBytes"] != len(longContent) {
		t.Fatalf("originalBytes = %d, want %d", meta["originalBytes"], len(longContent))
	}
	var contentBlocks []acp.ToolCallContent
	if err := json.Unmarshal(update.Content, &contentBlocks); err != nil {
		t.Fatal(err)
	}
	if len(contentBlocks) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contentBlocks))
	}
	if len(contentBlocks[0].Content.Text) > scfg.ToolPayloadHardMax {
		t.Fatalf("content text length %d exceeds hard limit", len(contentBlocks[0].Content.Text))
	}
	if update.RawOutput == nil {
		t.Fatal("rawOutput should be set in full mode")
	}
}

func TestUpdateFromTranscriptEvent_ToolResultCompactHardLimit(t *testing.T) {
	scfg := SessionConfig{
		ToolEvents:         ToolEventsCompact,
		ToolResultMaxBytes: 200,
		ToolPayloadHardMax: 50,
	}

	longContent := strings.Repeat("x", 100)
	event := claude.ToolResultEvent{
		ToolUseID: "tool-1",
		Content:   json.RawMessage(`"` + longContent + `"`),
		IsError:   false,
	}
	update, ok := updateFromTranscriptEvent(event, scfg)
	if !ok {
		t.Fatal("expected ok")
	}
	meta := update.Meta
	if meta == nil || meta["truncated"] != true {
		t.Fatal("expected truncated=true in meta")
	}
	var contentBlocks []acp.ToolCallContent
	if err := json.Unmarshal(update.Content, &contentBlocks); err != nil {
		t.Fatal(err)
	}
	if len(contentBlocks) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contentBlocks))
	}
	if len(contentBlocks[0].Content.Text) > scfg.ToolPayloadHardMax {
		t.Fatalf("content text length %d exceeds hard limit", len(contentBlocks[0].Content.Text))
	}
	if update.RawOutput != nil {
		t.Fatal("rawOutput should be nil in compact mode")
	}
}

func TestUpdateFromTranscriptEvent_ToolEventsOff(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsOff}

	toolUse := claude.AssistantToolUseEvent{ToolUseID: "t1", Name: "Read", Input: json.RawMessage(`{}`)}
	if _, ok := updateFromTranscriptEvent(toolUse, scfg); ok {
		t.Fatal("tool_call should be suppressed when off")
	}

	toolResult := claude.ToolResultEvent{ToolUseID: "t1", Content: json.RawMessage(`"ok"`)}
	if _, ok := updateFromTranscriptEvent(toolResult, scfg); ok {
		t.Fatal("tool_call_update should be suppressed when off")
	}
}

func TestUpdateFromTranscriptEvent_UnknownEvent(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsCompact}
	unknown := claude.SessionTitleEvent{Title: "ignored"}
	if _, ok := updateFromTranscriptEvent(unknown, scfg); ok {
		t.Fatal("unknown events should be skipped")
	}
}

func TestUpdateFromTranscriptEvent_EmptyText(t *testing.T) {
	scfg := SessionConfig{ToolEvents: ToolEventsCompact}
	event := claude.AssistantTextEvent{MessageID: "msg-1", Text: ""}
	if _, ok := updateFromTranscriptEvent(event, scfg); ok {
		t.Fatal("empty text event without stop reason should be skipped")
	}
}

func TestBuildToolTitle(t *testing.T) {
	tests := []struct {
		name  string
		event claude.AssistantToolUseEvent
		want  string
	}{
		{"with file_path", claude.AssistantToolUseEvent{Name: "Read", Input: json.RawMessage(`{"file_path":"/tmp/a.txt"}`)}, "Read /tmp/a.txt"},
		{"with path", claude.AssistantToolUseEvent{Name: "Write", Input: json.RawMessage(`{"path":"/tmp/b.txt"}`)}, "Write /tmp/b.txt"},
		{"with command", claude.AssistantToolUseEvent{Name: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)}, "Bash ls -la"},
		{"with url", claude.AssistantToolUseEvent{Name: "Fetch", Input: json.RawMessage(`{"url":"https://example.com"}`)}, "Fetch https://example.com"},
		{"unknown tool", claude.AssistantToolUseEvent{Name: "CustomTool", Input: json.RawMessage(`{"x":"y"}`)}, "CustomTool"},
		{"empty name", claude.AssistantToolUseEvent{Name: "", Input: json.RawMessage(`{}`)}, "tool"},
		{"empty input", claude.AssistantToolUseEvent{Name: "Read", Input: nil}, "Read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildToolTitle(tt.event)
			if got != tt.want {
				t.Fatalf("buildToolTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResultContentString(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
		want  string
	}{
		{"nil", nil, ""},
		{"empty", json.RawMessage{}, ""},
		{"string", json.RawMessage(`"hello"`), "hello"},
		{"text block array", json.RawMessage(`[{"type":"text","text":"hi"},{"type":"text","text":"there"}]`), "hi\nthere"},
		{"empty block array", json.RawMessage(`[]`), ""},
		{"object fallback", json.RawMessage(`{"key":"value"}`), `{"key":"value"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resultContentString(tt.input)
			if got != tt.want {
				t.Fatalf("resultContentString = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateJSONPreview(t *testing.T) {
	short := json.RawMessage(`{"file_path":"/tmp/a.txt"}`)
	result := truncateJSONPreview(short, 100)
	if string(result) != string(short) {
		t.Fatal("short input should pass through unchanged")
	}

	long := json.RawMessage(`{"file_path":"` + strings.Repeat("x", 100) + `"}`)
	result = truncateJSONPreview(long, 20)
	var preview string
	if err := json.Unmarshal(result, &preview); err != nil {
		t.Fatalf("result should be JSON string: %s", string(result))
	}
	if !strings.HasSuffix(preview, "...") {
		t.Fatalf("preview should end with ...: %q", preview)
	}
}

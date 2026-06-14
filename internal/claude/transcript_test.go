package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAssistantLine(t *testing.T) {
	line := []byte(`{
		"type": "assistant",
		"timestamp": "2026-06-12T12:00:00Z",
		"message": {
			"role": "assistant",
			"stop_reason": "end_turn",
			"content": [
				{"type": "text", "text": "hello"},
				{"type": "tool_use", "name": "ignored"},
				{"type": "text", "text": "world"}
			]
		}
	}`)

	msg, ok := parseAssistantLine(line)
	if !ok {
		t.Fatal("expected assistant line to parse")
	}
	if msg.Text != "hello\nworld" {
		t.Fatalf("text = %q", msg.Text)
	}
	if msg.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q", msg.StopReason)
	}
}

func TestParseTranscriptEventsFromFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "transcripts", "text_tool_interleaving.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var events []TranscriptEvent
	var offset int64
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		events = append(events, parseTranscriptEvents([]byte(line+"\n"), offset)...)
		offset += int64(len(line) + 1)
	}

	if len(events) != 4 {
		t.Fatalf("events = %#v", events)
	}
	firstText, ok := events[0].(AssistantTextEvent)
	if !ok || firstText.MessageID != "msg-1" || firstText.Text != "I'll inspect it." {
		t.Fatalf("first event = %#v", events[0])
	}
	toolUse, ok := events[1].(AssistantToolUseEvent)
	if !ok || toolUse.ToolUseID != "tool-1" || toolUse.Name != "Read" || !json.Valid(toolUse.Input) {
		t.Fatalf("tool event = %#v", events[1])
	}
	result, ok := events[2].(ToolResultEvent)
	if !ok || result.ToolUseID != "tool-1" || result.IsError {
		t.Fatalf("tool result = %#v", events[2])
	}
	lastText, ok := events[3].(AssistantTextEvent)
	if !ok || lastText.Text != "Done." || lastText.StopReason != "end_turn" {
		t.Fatalf("last event = %#v", events[3])
	}
}

func TestTranscriptReaderReadNewEventsReturnsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := TranscriptReader{}
	events, offset, err := reader.ReadNewEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if offset == 0 || len(events) != 1 {
		t.Fatalf("events=%#v offset=%d", events, offset)
	}
	diagnostic, ok := events[0].(TranscriptDiagnosticEvent)
	if !ok || diagnostic.EventType != "invalid_json" || diagnostic.ByteOffset != 0 {
		t.Fatalf("diagnostic = %#v", events[0])
	}
}

func TestTranscriptReaderReadNewTracksOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	first := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"}]}}` + "\n"
	second := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"two"}]}}` + "\n"

	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := TranscriptReader{}
	messages, offset, err := reader.ReadNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if offset == 0 || len(messages) != 1 || messages[0].Text != "one" || messages[0].ByteOffset != 0 {
		t.Fatalf("first read messages=%v offset=%d", messages, offset)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(second); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	messages, _, err = reader.ReadNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Text != "two" {
		t.Fatalf("second read messages=%v", messages)
	}
}

func TestTranscriptReaderKeepsPartialLineUnread(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	partial := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"}]}}`

	if err := os.WriteFile(path, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := TranscriptReader{}
	messages, offset, err := reader.ReadNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 || offset != 0 {
		t.Fatalf("partial read messages=%v offset=%d", messages, offset)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	messages, offset, err = reader.ReadNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Text != "one" || offset == 0 {
		t.Fatalf("completed read messages=%v offset=%d", messages, offset)
	}
}

func TestTranscriptReaderRejectsHugeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	line := strings.Repeat("x", maxTranscriptLineBytes+1) + "\n"

	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := TranscriptReader{}
	if _, _, err := reader.ReadNew(path); err == nil {
		t.Fatal("expected huge transcript line to fail")
	}
}

func TestJoinAssistantText(t *testing.T) {
	text := joinAssistantText([]AssistantMessage{
		{Text: " one "},
		{Text: ""},
		{Text: "two"},
	})
	if text != "one\ntwo" || strings.Contains(text, "\n\n") {
		t.Fatalf("joined text = %q", text)
	}
}

package app

import (
	"strings"
	"testing"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func TestPromptTextConvertsResourceLink(t *testing.T) {
	text, err := promptText([]acp.ContentBlock{{Type: "resource_link", URI: "file:///tmp/a.txt", Title: "A", MimeType: "text/plain"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"file:///tmp/a.txt", "title: A", "mime: text/plain"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q", text)
		}
	}
}

func TestPromptTextRejectsResourceLinkWithoutURI(t *testing.T) {
	_, err := promptText([]acp.ContentBlock{{Type: "resource_link", Title: "missing"}})
	if err == nil || !strings.Contains(err.Error(), "uri is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestStopReasonMapping(t *testing.T) {
	tests := []struct {
		name      string
		response  claude.Response
		cancelled bool
		want      acp.StopReason
		wantErr   string
	}{
		{name: "cancelled", cancelled: true, want: "cancelled"},
		{name: "max tokens", response: claude.Response{Text: "x", Messages: []claude.AssistantMessage{{StopReason: "max_tokens"}}}, want: "max_tokens"},
		{name: "stop sequence", response: claude.Response{Text: "x", Messages: []claude.AssistantMessage{{StopReason: "stop_sequence"}}}, want: "end_turn"},
		{name: "unknown with text", response: claude.Response{Text: "x", Messages: []claude.AssistantMessage{{StopReason: "new_reason"}}}, want: "end_turn"},
		{name: "unknown without text", response: claude.Response{Messages: []claude.AssistantMessage{{StopReason: "new_reason"}}}, wantErr: "unsupported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stopReason(tt.response, tt.cancelled)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("stop reason = %q", got)
			}
		})
	}
}

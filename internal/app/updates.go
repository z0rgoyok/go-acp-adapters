package app

import (
	"strings"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func updateFromTranscriptEvent(event claude.TranscriptEvent) (acp.SessionUpdate, bool) {
	text, ok := event.(claude.AssistantTextEvent)
	if !ok || strings.TrimSpace(text.Text) == "" {
		return acp.SessionUpdate{}, false
	}
	return acp.SessionUpdate{
		SessionUpdate: "agent_message_chunk",
		MessageID:     text.MessageID,
		Content:       acp.ContentBlock{Type: "text", Text: text.Text},
	}, true
}

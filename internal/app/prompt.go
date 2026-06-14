package app

import (
	"strings"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func promptText(blocks []acp.ContentBlock) (string, error) {
	if len(blocks) == 0 {
		return "", invalidParams("prompt is empty")
	}
	var parts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		case "resource_link":
			if strings.TrimSpace(block.URI) == "" {
				return "", invalidParams("resource_link uri is required")
			}
			parts = append(parts, resourceLinkText(block))
		default:
			return "", invalidParams("unsupported content block: " + block.Type)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if text == "" {
		return "", invalidParams("prompt is empty")
	}
	return text, nil
}

func resourceLinkText(block acp.ContentBlock) string {
	var fields []string
	fields = append(fields, "uri: "+block.URI)
	if block.Title != "" {
		fields = append(fields, "title: "+block.Title)
	}
	if block.MimeType != "" {
		fields = append(fields, "mime: "+block.MimeType)
	}
	return "Resource link (" + strings.Join(fields, ", ") + ")"
}

func stopReason(response claude.Response, cancelled bool) (acp.StopReason, error) {
	if cancelled {
		return acp.StopReasonCancelled, nil
	}
	if len(response.Messages) == 0 {
		if strings.TrimSpace(response.Text) == "" {
			return "", internalError("Claude response did not contain assistant text")
		}
		return acp.StopReasonEndTurn, nil
	}
	switch response.Messages[len(response.Messages)-1].StopReason {
	case "", "end_turn", "stop_sequence":
		return acp.StopReasonEndTurn, nil
	case "max_tokens":
		return acp.StopReasonMaxTokens, nil
	case "max_turn_requests":
		return acp.StopReasonMaxTurnRequests, nil
	case "refusal":
		return acp.StopReasonRefusal, nil
	default:
		if strings.TrimSpace(response.Text) != "" {
			return acp.StopReasonEndTurn, nil
		}
		return "", internalError("unsupported Claude stop reason")
	}
}

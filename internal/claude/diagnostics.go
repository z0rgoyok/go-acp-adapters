package claude

import (
	"fmt"
	"os"
)

func emitTranscriptDiagnostic(path string, event TranscriptEvent) {
	if !transcriptDiagnosticsEnabled() {
		return
	}
	eventType, offset, ok := diagnosticEventInfo(event)
	if !ok {
		return
	}
	fmt.Fprintf(os.Stderr, "transcript path=%q offset=%d event=%s\n", path, offset, eventType)
}

func transcriptDiagnosticsEnabled() bool {
	return os.Getenv("CLAUDE_ACP_DEBUG_TRANSCRIPT") == "1"
}

func diagnosticEventInfo(event TranscriptEvent) (string, int64, bool) {
	switch typed := event.(type) {
	case AssistantToolUseEvent:
		return "assistant.tool_use", typed.ByteOffset, true
	case ToolResultEvent:
		return "user.tool_result", typed.ByteOffset, true
	case SessionTitleEvent:
		return "ai-title", typed.ByteOffset, true
	case UsageEvent:
		return "assistant.usage", typed.ByteOffset, true
	case UnknownTranscriptEvent:
		return typed.EventType, typed.ByteOffset, true
	case TranscriptDiagnosticEvent:
		return typed.EventType, typed.ByteOffset, true
	default:
		return "", 0, false
	}
}

package claude

import (
	"os"
	"time"

	"github.com/google/uuid"
)

type turnContext struct {
	ID             string
	SessionID      string
	TranscriptPath string
	StartOffset    int64
	StartedAt      time.Time
}

func newTurnContext(sessionID string, transcript *TranscriptReader, transcriptPath string) turnContext {
	return turnContext{
		ID:             uuid.NewString(),
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
		StartOffset:    turnStartOffset(transcript, transcriptPath),
		StartedAt:      time.Now(),
	}
}

func turnStartOffset(transcript *TranscriptReader, transcriptPath string) int64 {
	if transcriptPath != "" {
		if info, err := os.Stat(transcriptPath); err == nil {
			return info.Size()
		}
	}
	return transcript.offset
}

func currentTurnStop(payload stopPayload, turn turnContext) bool {
	return payload.TurnID != "" && payload.TurnID == turn.ID
}

func writeCurrentTurn(path string, turn turnContext) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(turn.ID), 0o600)
}

func eventOffset(event TranscriptEvent) int64 {
	switch typed := event.(type) {
	case AssistantTextEvent:
		return typed.ByteOffset
	case AssistantToolUseEvent:
		return typed.ByteOffset
	case ToolResultEvent:
		return typed.ByteOffset
	case SessionTitleEvent:
		return typed.ByteOffset
	case UsageEvent:
		return typed.ByteOffset
	case UnknownTranscriptEvent:
		return typed.ByteOffset
	case TranscriptDiagnosticEvent:
		return typed.ByteOffset
	default:
		return 0
	}
}

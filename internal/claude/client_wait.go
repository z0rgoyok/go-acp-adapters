package claude

import (
	"context"
	"time"
)

func waitForTranscript(ctx context.Context, stopReader *stopReader, configDir, sessionID string, turn turnContext) (string, error) {
	select {
	case payload, ok := <-stopReader.Payloads():
		if ok && payload.TranscriptPath != "" && payloadMatchesSession(payload, sessionID) && currentTurnStop(payload, turn) {
			return payload.TranscriptPath, nil
		}
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}
	path, err := findTranscript(ctx, transcriptRoots(configDir), sessionID, 10*time.Second)
	return path, err
}

func findTranscriptPath(roots []string, sessionID string) string {
	for _, root := range roots {
		if path := findTranscriptOnce(root, sessionID); path != "" {
			return path
		}
	}
	return ""
}

func payloadMatchesSession(payload stopPayload, sessionID string) bool {
	return payload.SessionID == "" || payload.SessionID == sessionID
}

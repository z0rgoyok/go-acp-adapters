package claude

import (
	"context"
	"time"
)

func waitForAssistantMessages(ctx context.Context, stopReader *stopReader, transcript *TranscriptReader, path string, turn turnContext, out chan<- TranscriptEvent) ([]AssistantMessage, error) {
	var collected []AssistantMessage
	for {
		events, _, err := transcript.ReadNewEvents(path)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if eventOffset(event) < turn.StartOffset {
				continue
			}
			emitTranscriptDiagnostic(path, event)
			if out != nil {
				select {
				case out <- event:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			text, ok := event.(AssistantTextEvent)
			if !ok {
				continue
			}
			if text.StopReason == "tool_use" && text.Text == "" {
				continue
			}
			collected = append(collected, AssistantMessage{
				ByteOffset: text.ByteOffset,
				MessageID:  text.MessageID,
				Text:       text.Text,
				StopReason: text.StopReason,
				Timestamp:  text.Timestamp,
			})
		}
		if hasTerminalAssistant(collected) {
			return collected, nil
		}

		select {
		case payload, ok := <-stopReader.Payloads():
			if ok && currentTurnStop(payload, turn) {
				continue
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

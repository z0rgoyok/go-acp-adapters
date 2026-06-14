package claude

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const maxTranscriptLineBytes = 1024 * 1024

type TranscriptReader struct {
	offset int64
}

func (r *TranscriptReader) ReadNew(path string) ([]AssistantMessage, int64, error) {
	events, offset, err := r.ReadNewEvents(path)
	if err != nil {
		return nil, offset, err
	}
	var messages []AssistantMessage
	for _, event := range events {
		text, ok := event.(AssistantTextEvent)
		if !ok {
			continue
		}
		messages = append(messages, AssistantMessage{
			ByteOffset: text.ByteOffset,
			MessageID:  text.MessageID,
			Text:       text.Text,
			StopReason: text.StopReason,
			Timestamp:  text.Timestamp,
		})
	}
	return messages, offset, nil
}

func (r *TranscriptReader) ReadNewEvents(path string) ([]TranscriptEvent, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, r.offset, err
	}
	defer file.Close()

	if r.offset > 0 {
		if _, err := file.Seek(r.offset, io.SeekStart); err != nil {
			return nil, r.offset, err
		}
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	var events []TranscriptEvent
	nextOffset := r.offset
	for {
		line, complete, err := readTranscriptLine(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, r.offset, err
		}
		if !complete {
			break
		}
		lineOffset := nextOffset
		nextOffset += int64(len(line))
		events = append(events, parseTranscriptEvents(line, lineOffset)...)
	}
	r.offset = nextOffset
	return events, nextOffset, nil
}

func readTranscriptLine(reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			if len(line)+len(fragment) > maxTranscriptLineBytes {
				return nil, false, fmt.Errorf("transcript line exceeds %d bytes", maxTranscriptLineBytes)
			}
			line = append(line, fragment...)
		}
		if err == nil {
			return line, true, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err == io.EOF {
			return nil, false, io.EOF
		}
		return nil, false, err
	}
}

func (r *TranscriptReader) WaitAndReadNew(path string, minSize int64, timeout time.Duration) ([]AssistantMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		info, err := os.Stat(path)
		if err == nil && info.Size() >= minSize {
			return r.readAfterFlush(path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return r.readAfterFlush(path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *TranscriptReader) readAfterFlush(path string) ([]AssistantMessage, error) {
	time.Sleep(250 * time.Millisecond)
	messages, _, err := r.ReadNew(path)
	return messages, err
}

func joinAssistantText(messages []AssistantMessage) string {
	var parts []string
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func hasTerminalAssistant(messages []AssistantMessage) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	return last.StopReason == "end_turn" ||
		last.StopReason == "max_tokens" ||
		last.StopReason == "max_turn_requests" ||
		last.StopReason == "refusal" ||
		last.StopReason == "stop_sequence"
}

func parseAssistantLine(line []byte) (AssistantMessage, bool) {
	var parts []string
	var message AssistantMessage
	for _, event := range parseTranscriptEvents(line, 0) {
		text, ok := event.(AssistantTextEvent)
		if !ok {
			continue
		}
		if text.Text != "" {
			parts = append(parts, text.Text)
		}
		message = AssistantMessage{ByteOffset: text.ByteOffset, MessageID: text.MessageID, Text: text.Text, StopReason: text.StopReason, Timestamp: text.Timestamp}
	}
	if len(parts) == 0 {
		return AssistantMessage{}, false
	}
	message.Text = strings.Join(parts, "\n")
	return message, true
}

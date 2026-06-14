package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

type fakeTransport struct {
	mu           sync.Mutex
	response     claude.Response
	prompt       string
	queried      bool
	cancelled    bool
	disconnected bool
	block        chan struct{}
	queryStarted chan struct{}
	connectErr   error
	cancelErr    error
	streamEvents []claude.TranscriptEvent
}

func (f *fakeTransport) Connect(context.Context) error { return f.connectErr }

func (f *fakeTransport) Query(ctx context.Context, prompt string) (claude.Response, error) {
	f.mu.Lock()
	f.prompt = prompt
	f.queried = true
	if f.queryStarted != nil {
		close(f.queryStarted)
	}
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-ctx.Done():
			return claude.Response{}, ctx.Err()
		case <-f.block:
		}
	}
	return f.response, nil
}

func (f *fakeTransport) StartTurn(ctx context.Context, prompt string) claude.TurnStream {
	events := make(chan claude.TranscriptEvent, 8)
	done := make(chan claude.TurnResult, 1)
	go func() {
		defer close(events)
		response, err := f.Query(ctx, prompt)
		for _, event := range f.eventsForResponse(response) {
			select {
			case events <- event:
			case <-ctx.Done():
				done <- claude.TurnResult{Response: response, Err: ctx.Err()}
				close(done)
				return
			}
		}
		done <- claude.TurnResult{Response: response, Err: err}
		close(done)
	}()
	return claude.TurnStream{Events: events, Done: done}
}

func (f *fakeTransport) eventsForResponse(response claude.Response) []claude.TranscriptEvent {
	if f.streamEvents != nil {
		return f.streamEvents
	}
	events := make([]claude.TranscriptEvent, 0, len(response.Messages))
	for _, message := range response.Messages {
		if strings.TrimSpace(message.Text) == "" {
			continue
		}
		events = append(events, claude.AssistantTextEvent{ByteOffset: message.ByteOffset, Timestamp: message.Timestamp, MessageID: message.MessageID, Text: message.Text, StopReason: message.StopReason})
	}
	return events
}

func (f *fakeTransport) Cancel(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
	return f.cancelErr
}

func (f *fakeTransport) Disconnect(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnected = true
}

func (f *fakeTransport) SessionID() string { return "claude-session" }

func (f *fakeTransport) waitQuery(t *testing.T) {
	t.Helper()
	select {
	case <-f.queryStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for query")
	}
}

type fakeNotifier struct {
	updates []acp.SessionUpdateParams
}

func (n *fakeNotifier) SessionUpdate(params acp.SessionUpdateParams) error {
	n.updates = append(n.updates, params)
	return nil
}

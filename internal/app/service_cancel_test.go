package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func TestCancelWaitsForInterruptResultAfterQueryReturns(t *testing.T) {
	fake := newInterruptingTransport(nil)
	service := NewService(Options{Factory: func(TransportOptions) (Transport, error) { return fake, nil }})
	session := mustSession(t, service)
	done := make(chan acp.PromptResponse, 1)

	go func() {
		response, _ := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		done <- response
	}()
	fake.waitQuery(t)
	cancelDone := make(chan error, 1)
	go func() {
		cancelDone <- service.CancelSession(context.Background(), acp.CancelSessionRequest{SessionID: session})
	}()
	fake.waitInterrupted(t)

	select {
	case response := <-done:
		t.Fatalf("prompt completed before cancel result: %+v", response)
	case <-time.After(10 * time.Millisecond):
	}
	close(fake.allowCancelReturn)
	if err := <-cancelDone; err != nil {
		t.Fatal(err)
	}
	if response := <-done; response.StopReason != "cancelled" {
		t.Fatalf("stop reason = %q", response.StopReason)
	}
}

func TestCancelFailureWinsAfterQueryReturnsFromInterrupt(t *testing.T) {
	fake := newInterruptingTransport(errors.New("interrupt failed"))
	service := NewService(Options{Factory: func(TransportOptions) (Transport, error) { return fake, nil }})
	session := mustSession(t, service)
	done := make(chan error, 1)

	go func() {
		_, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		done <- err
	}()
	fake.waitQuery(t)
	cancelDone := make(chan error, 1)
	go func() {
		cancelDone <- service.CancelSession(context.Background(), acp.CancelSessionRequest{SessionID: session})
	}()
	fake.waitInterrupted(t)

	select {
	case err := <-done:
		t.Fatalf("prompt completed before cancel result: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(fake.allowCancelReturn)
	if err := <-cancelDone; err == nil || !strings.Contains(err.Error(), "cancellation failure") {
		t.Fatalf("cancel err = %v", err)
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "cancellation failure") {
		t.Fatalf("prompt err = %v", err)
	}
}

type interruptingTransport struct {
	queryStarted      chan struct{}
	interrupted       chan struct{}
	allowCancelReturn chan struct{}
	cancelErr         error
}

func newInterruptingTransport(cancelErr error) *interruptingTransport {
	return &interruptingTransport{
		queryStarted:      make(chan struct{}),
		interrupted:       make(chan struct{}),
		allowCancelReturn: make(chan struct{}),
		cancelErr:         cancelErr,
	}
}

func (t *interruptingTransport) Connect(context.Context) error { return nil }

func (t *interruptingTransport) Query(context.Context, string) (claude.Response, error) {
	close(t.queryStarted)
	<-t.interrupted
	return claude.Response{}, errors.New("interrupted")
}

func (t *interruptingTransport) StartTurn(ctx context.Context, prompt string) claude.TurnStream {
	events := make(chan claude.TranscriptEvent)
	done := make(chan claude.TurnResult, 1)
	go func() {
		defer close(events)
		response, err := t.Query(ctx, prompt)
		done <- claude.TurnResult{Response: response, Err: err}
		close(done)
	}()
	return claude.TurnStream{Events: events, Done: done}
}

func (t *interruptingTransport) Cancel(context.Context) error {
	close(t.interrupted)
	<-t.allowCancelReturn
	return t.cancelErr
}

func (t *interruptingTransport) Disconnect(context.Context) {}

func (t *interruptingTransport) SessionID() string { return "claude-session" }

func (t *interruptingTransport) waitQuery(tst *testing.T) {
	tst.Helper()
	select {
	case <-t.queryStarted:
	case <-time.After(time.Second):
		tst.Fatal("timed out waiting for query")
	}
}

func (t *interruptingTransport) waitInterrupted(tst *testing.T) {
	tst.Helper()
	select {
	case <-t.interrupted:
	case <-time.After(time.Second):
		tst.Fatal("timed out waiting for interrupt")
	}
}

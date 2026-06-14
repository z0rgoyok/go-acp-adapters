package acp

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestServerReadsCancelWhilePromptRunning(t *testing.T) {
	in, writer := io.Pipe()
	out := &bytes.Buffer{}
	backend := &blockingBackend{
		promptStarted: make(chan struct{}),
		cancelCalled:  make(chan struct{}),
	}
	server := NewServer(backend, in, out, &bytes.Buffer{})
	done := make(chan error, 1)

	go func() { done <- server.Serve(context.Background()) }()
	writeLine(t, writer, `{"id":"p1","method":"session/prompt","params":{"sessionId":"s1","prompt":[{"type":"text","text":"wait"}]}}`)
	waitFor(t, backend.promptStarted, "prompt start")
	writeLine(t, writer, `{"method":"session/cancel","params":{"sessionId":"s1"}}`)
	waitFor(t, backend.cancelCalled, "cancel")

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"stopReason":"cancelled"`) {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestServerDoesNotLoseImmediateCancelAfterPrompt(t *testing.T) {
	in, writer := io.Pipe()
	out := &bytes.Buffer{}
	backend := &blockingBackend{cancelCalled: make(chan struct{})}
	server := NewServer(backend, in, out, &bytes.Buffer{})
	done := make(chan error, 1)

	go func() { done <- server.Serve(context.Background()) }()
	writeLine(t, writer, `{"id":"p1","method":"session/prompt","params":{"sessionId":"s1","prompt":[{"type":"text","text":"wait"}]}}`)
	writeLine(t, writer, `{"method":"session/cancel","params":{"sessionId":"s1"}}`)
	waitFor(t, backend.cancelCalled, "cancel")

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"stopReason":"cancelled"`) {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestServerShutdownsPromptWhenInputCloses(t *testing.T) {
	in, writer := io.Pipe()
	out := &bytes.Buffer{}
	backend := &blockingBackend{
		promptStarted:  make(chan struct{}),
		shutdownCalled: make(chan struct{}),
	}
	server := NewServer(backend, in, out, &bytes.Buffer{})
	done := make(chan error, 1)

	go func() { done <- server.Serve(context.Background()) }()
	writeLine(t, writer, `{"id":"p1","method":"session/prompt","params":{"sessionId":"s1","prompt":[{"type":"text","text":"wait"}]}}`)
	waitFor(t, backend.promptStarted, "prompt start")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, backend.shutdownCalled, "shutdown")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"stopReason":"cancelled"`) {
		t.Fatalf("stdout = %q", out.String())
	}
}

type blockingBackend struct {
	mu             sync.Mutex
	cancel         context.CancelFunc
	promptStarted  chan struct{}
	cancelCalled   chan struct{}
	shutdownCalled chan struct{}
	promptOnce     sync.Once
	cancelOnce     sync.Once
	shutdownOnce   sync.Once
}

type blockingPromptTurn struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (b *blockingBackend) Initialize(context.Context, InitializeRequest) (InitializeResponse, error) {
	return InitializeResponse{ProtocolVersion: ProtocolVersion}, nil
}

func (b *blockingBackend) NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error) {
	return NewSessionResponse{SessionID: "s1"}, nil
}

func (b *blockingBackend) SetSessionModel(context.Context, SetSessionModelRequest) (SetSessionModelResponse, error) {
	return SetSessionModelResponse{}, nil
}

func (b *blockingBackend) SetSessionConfigOption(context.Context, SetSessionConfigOptionRequest) (SetSessionConfigOptionResponse, error) {
	return SetSessionConfigOptionResponse{}, nil
}

func (b *blockingBackend) Prompt(ctx context.Context, _ PromptRequest, _ Notifier) (PromptResponse, error) {
	turn, _ := b.StartPrompt(ctx, PromptRequest{})
	return turn.Run(ctx, nil)
}

func (b *blockingBackend) StartPrompt(ctx context.Context, _ PromptRequest) (PromptTurn, error) {
	promptCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
	if b.promptStarted != nil {
		b.promptOnce.Do(func() { close(b.promptStarted) })
	}
	return &blockingPromptTurn{ctx: promptCtx, cancel: cancel}, nil
}

func (t *blockingPromptTurn) Run(context.Context, Notifier) (PromptResponse, error) {
	defer t.cancel()
	<-t.ctx.Done()
	return PromptResponse{StopReason: "cancelled"}, nil
}

func (b *blockingBackend) CancelSession(context.Context, CancelSessionRequest) error {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if b.cancelCalled != nil {
		b.cancelOnce.Do(func() { close(b.cancelCalled) })
	}
	return nil
}

func (b *blockingBackend) CloseSession(context.Context, CloseSessionRequest) error { return nil }

func (b *blockingBackend) Shutdown(context.Context) error {
	if b.shutdownCalled != nil {
		b.shutdownOnce.Do(func() { close(b.shutdownCalled) })
	}
	return nil
}

func writeLine(t *testing.T, writer *io.PipeWriter, line string) {
	t.Helper()
	if _, err := writer.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

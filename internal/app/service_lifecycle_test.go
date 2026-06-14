package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"claude-acp-adapter/internal/acp"
)

func TestNewSessionCleansUpAfterConnectError(t *testing.T) {
	fake := &fakeTransport{connectErr: errors.New("boom")}
	var options TransportOptions
	service := NewService(Options{Factory: func(actual TransportOptions) (Transport, error) {
		options = actual
		return fake, nil
	}})

	_, err := service.NewSession(context.Background(), acp.NewSessionRequest{
		Cwd:        t.TempDir(),
		McpServers: []acp.McpServer{{Name: "fs", Type: "stdio", Command: "node"}},
	})
	if err == nil || !strings.Contains(err.Error(), "startup failure") {
		t.Fatalf("err = %v", err)
	}
	if !fake.disconnected {
		t.Fatal("transport was not disconnected")
	}
	if len(options.ExtraArgs) != 2 {
		t.Fatalf("extra args = %+v", options.ExtraArgs)
	}
	if _, statErr := os.Stat(options.ExtraArgs[1]); !os.IsNotExist(statErr) {
		t.Fatalf("mcp config still exists: %v", statErr)
	}
	_, err = service.Prompt(context.Background(), acp.PromptRequest{SessionID: "missing", Prompt: []acp.ContentBlock{{Type: "text", Text: "hi"}}}, &fakeNotifier{})
	if err == nil || !strings.Contains(err.Error(), "unknown session") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseRemovesSessionAndDisconnects(t *testing.T) {
	fake := &fakeTransport{}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)

	if err := service.CloseSession(context.Background(), acp.CloseSessionRequest{SessionID: session}); err != nil {
		t.Fatal(err)
	}
	if !fake.disconnected {
		t.Fatal("transport was not disconnected")
	}
	_, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "hi"}}}, &fakeNotifier{})
	if err == nil || !strings.Contains(err.Error(), "unknown session") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseCancelsActiveTurnDisconnectsAndRemovesRegistry(t *testing.T) {
	fake := &fakeTransport{block: make(chan struct{}), queryStarted: make(chan struct{})}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)
	done := make(chan acp.PromptResponse, 1)

	go func() {
		response, _ := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		done <- response
	}()
	fake.waitQuery(t)
	if err := service.CloseSession(context.Background(), acp.CloseSessionRequest{SessionID: session}); err != nil {
		t.Fatal(err)
	}

	response := <-done
	if response.StopReason != "cancelled" {
		t.Fatalf("stop reason = %q", response.StopReason)
	}
	if !fake.cancelled || !fake.disconnected {
		t.Fatalf("cancelled=%v disconnected=%v", fake.cancelled, fake.disconnected)
	}
	if _, ok := service.registry.Get(session); ok {
		t.Fatal("session still registered")
	}
}

func TestShutdownDisconnectsAllSessionsAndRejectsNewPrompts(t *testing.T) {
	first := &fakeTransport{}
	second := &fakeTransport{}
	transports := []*fakeTransport{first, second}
	service := NewService(Options{Factory: func(TransportOptions) (Transport, error) {
		transport := transports[0]
		transports = transports[1:]
		return transport, nil
	}})
	firstSession := mustSession(t, service)
	secondSession := mustSession(t, service)

	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !first.disconnected || !second.disconnected {
		t.Fatalf("first disconnected=%v second disconnected=%v", first.disconnected, second.disconnected)
	}
	if _, ok := service.registry.Get(firstSession); ok {
		t.Fatal("first session still registered")
	}
	if _, ok := service.registry.Get(secondSession); ok {
		t.Fatal("second session still registered")
	}
	_, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: firstSession, Prompt: []acp.ContentBlock{{Type: "text", Text: "hi"}}}, &fakeNotifier{})
	if err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("err = %v", err)
	}
}

func mustSession(t *testing.T, service *Service) string {
	t.Helper()
	response, err := service.NewSession(context.Background(), acp.NewSessionRequest{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return response.SessionID
}

func fakeFactory(fake *fakeTransport) TransportFactory {
	return func(TransportOptions) (Transport, error) {
		if fake == nil {
			fake = &fakeTransport{}
		}
		return fake, nil
	}
}

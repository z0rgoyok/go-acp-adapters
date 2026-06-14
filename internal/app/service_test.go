package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func TestInitializeReturnsACPv1CapabilityShape(t *testing.T) {
	service := NewService(Options{Factory: fakeFactory(nil)})

	response, err := service.Initialize(context.Background(), acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	capabilities := wire["agentCapabilities"].(map[string]any)
	sessionCapabilities := capabilities["sessionCapabilities"].(map[string]any)
	if _, ok := sessionCapabilities["cancel"]; ok {
		t.Fatalf("sessionCapabilities.cancel must not be advertised: %s", data)
	}
	closeCapability, ok := sessionCapabilities["close"].(map[string]any)
	if !ok || len(closeCapability) != 0 {
		t.Fatalf("sessionCapabilities.close = %#v, want empty capability object", sessionCapabilities["close"])
	}
}

func TestNewSessionRejectsRelativeCwd(t *testing.T) {
	service := NewService(Options{Factory: fakeFactory(nil)})
	_, err := service.NewSession(context.Background(), acp.NewSessionRequest{Cwd: "relative"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewSessionRejectsRelativeAdditionalDirectory(t *testing.T) {
	factoryCalled := false
	service := NewService(Options{Factory: func(TransportOptions) (Transport, error) {
		factoryCalled = true
		return &fakeTransport{}, nil
	}})

	_, err := service.NewSession(context.Background(), acp.NewSessionRequest{Cwd: t.TempDir(), AdditionalDirectories: []string{"relative"}})
	if err == nil || !strings.Contains(err.Error(), "additionalDirectories") {
		t.Fatalf("err = %v", err)
	}
	if factoryCalled {
		t.Fatal("transport factory was called")
	}
}

func TestNewSessionReturnsMinimumSessionConfig(t *testing.T) {
	service := NewService(Options{Factory: fakeFactory(nil)})

	response, err := service.NewSession(context.Background(), acp.NewSessionRequest{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ConfigOptions) != 3 {
		t.Fatalf("config options = %+v", response.ConfigOptions)
	}
	wantCategories := map[string]string{"model": "model", "effort": "thought_level", "mode": "mode"}
	for _, option := range response.ConfigOptions {
		if wantCategories[option.ID] != option.Category || option.Type != "select" || len(option.Options) == 0 {
			t.Fatalf("option = %+v", option)
		}
	}
	if response.Modes == nil || response.Modes.CurrentModeID != "auto" || len(response.Modes.AvailableModes) != 1 || response.Modes.AvailableModes[0].ID != "auto" {
		t.Fatalf("modes = %+v", response.Modes)
	}
}

func TestSetSessionModelReplacesTransportWithRequestedModel(t *testing.T) {
	oldTransport := &fakeTransport{}
	newTransport := &fakeTransport{}
	transports := []*fakeTransport{oldTransport, newTransport}
	var options []TransportOptions
	service := NewService(Options{Factory: func(actual TransportOptions) (Transport, error) {
		options = append(options, actual)
		transport := transports[0]
		transports = transports[1:]
		return transport, nil
	}})
	session := mustSession(t, service)

	response, err := service.SetSessionModel(context.Background(), acp.SetSessionModelRequest{SessionID: session, ModelID: "claude-opus-4-8"})
	if err != nil {
		t.Fatal(err)
	}
	if response != (acp.SetSessionModelResponse{}) {
		t.Fatalf("response = %+v", response)
	}
	if len(options) != 2 || options[1].Model != "claude-opus-4-8" {
		t.Fatalf("options = %+v", options)
	}
	if !oldTransport.disconnected || newTransport.disconnected {
		t.Fatalf("old disconnected = %v, new disconnected = %v", oldTransport.disconnected, newTransport.disconnected)
	}
}

func TestSetSessionConfigOptionAcceptsMinimumConfig(t *testing.T) {
	transports := []*fakeTransport{{}, {}, {}}
	service := NewService(Options{Factory: func(TransportOptions) (Transport, error) {
		transport := transports[0]
		transports = transports[1:]
		return transport, nil
	}})
	sessionID := mustSession(t, service)

	for _, request := range []acp.SetSessionConfigOptionRequest{
		{SessionID: sessionID, ConfigID: "model", Value: "claude-sonnet-4-6"},
		{SessionID: sessionID, ConfigID: "effort", Value: "high"},
		{SessionID: sessionID, ConfigID: "mode", Value: "auto"},
	} {
		if _, err := service.SetSessionConfigOption(context.Background(), request); err != nil {
			t.Fatalf("%+v: %v", request, err)
		}
	}
}

func TestSetSessionConfigOptionRejectsActivePromptMutation(t *testing.T) {
	fake := &fakeTransport{block: make(chan struct{}), queryStarted: make(chan struct{})}
	service := NewService(Options{Factory: fakeFactory(fake)})
	sessionID := mustSession(t, service)
	done := make(chan struct{})

	go func() {
		_, _ = service.Prompt(context.Background(), acp.PromptRequest{SessionID: sessionID, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		close(done)
	}()
	fake.waitQuery(t)
	_, err := service.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{SessionID: sessionID, ConfigID: "effort", Value: "high"})
	if err == nil || !strings.Contains(err.Error(), "active prompt") {
		t.Fatalf("err = %v", err)
	}
	close(fake.block)
	<-done
}

func TestPromptSendsUpdateAndStopReason(t *testing.T) {
	fake := &fakeTransport{response: claude.Response{Text: "hello", Messages: []claude.AssistantMessage{{Text: "hello", StopReason: "end_turn", MessageID: "msg-1"}}}}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)
	notifier := &fakeNotifier{}

	response, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "hi"}}}, notifier)
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q", response.StopReason)
	}
	if fake.prompt != "hi" {
		t.Fatalf("prompt = %q", fake.prompt)
	}
	if len(notifier.updates) != 1 || notifier.updates[0].Update.SessionUpdate != "agent_message_chunk" || notifier.updates[0].Update.Content.Text != "hello" || notifier.updates[0].Update.MessageID != "msg-1" {
		t.Fatalf("updates = %+v", notifier.updates)
	}
}

func TestPromptStreamsMultipleTranscriptTextEvents(t *testing.T) {
	fake := &fakeTransport{
		streamEvents: []claude.TranscriptEvent{
			claude.AssistantTextEvent{MessageID: "msg-1", Text: "one"},
			claude.AssistantToolUseEvent{MessageID: "msg-1", ToolUseID: "tool-1", Name: "Read"},
			claude.ToolResultEvent{ToolUseID: "tool-1"},
			claude.AssistantTextEvent{MessageID: "msg-1", Text: "two", StopReason: "end_turn"},
		},
		response: claude.Response{Text: "one\ntwo", Messages: []claude.AssistantMessage{{Text: "one", MessageID: "msg-1"}, {Text: "two", MessageID: "msg-1", StopReason: "end_turn"}}},
	}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)
	notifier := &fakeNotifier{}

	response, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "hi"}}}, notifier)
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("stop reason = %q", response.StopReason)
	}
	if len(notifier.updates) != 2 {
		t.Fatalf("updates = %+v", notifier.updates)
	}
	for i, want := range []string{"one", "two"} {
		if notifier.updates[i].Update.Content.Text != want || notifier.updates[i].Update.MessageID != "msg-1" {
			t.Fatalf("update[%d] = %+v", i, notifier.updates[i])
		}
	}
}

func TestPromptRejectsUnsupportedContentBeforeTransport(t *testing.T) {
	fake := &fakeTransport{}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)

	_, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "image"}}}, &fakeNotifier{})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v", err)
	}
	if fake.queried {
		t.Fatal("transport was called")
	}
}

func TestCancelReturnsCancelledPrompt(t *testing.T) {
	fake := &fakeTransport{block: make(chan struct{}), queryStarted: make(chan struct{})}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)
	done := make(chan acp.PromptResponse, 1)

	go func() {
		response, _ := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		done <- response
	}()
	fake.waitQuery(t)
	if err := service.CancelSession(context.Background(), acp.CancelSessionRequest{SessionID: session}); err != nil {
		t.Fatal(err)
	}

	response := <-done
	if response.StopReason != "cancelled" {
		t.Fatalf("stop reason = %q", response.StopReason)
	}
	if !fake.cancelled {
		t.Fatal("transport was not cancelled")
	}
}

func TestCancelFailureDoesNotReportCancelledPrompt(t *testing.T) {
	fake := &fakeTransport{block: make(chan struct{}), queryStarted: make(chan struct{}), cancelErr: errors.New("interrupt failed")}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)
	done := make(chan error, 1)

	go func() {
		_, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		done <- err
	}()
	fake.waitQuery(t)
	if err := service.CancelSession(context.Background(), acp.CancelSessionRequest{SessionID: session}); err == nil || !strings.Contains(err.Error(), "cancellation failure") {
		t.Fatalf("cancel err = %v", err)
	}

	err := <-done
	if err == nil || !strings.Contains(err.Error(), "cancellation failure") {
		t.Fatalf("prompt err = %v", err)
	}
}

func TestPromptRejectsSecondActiveTurn(t *testing.T) {
	fake := &fakeTransport{block: make(chan struct{}), queryStarted: make(chan struct{})}
	service := NewService(Options{Factory: fakeFactory(fake)})
	session := mustSession(t, service)
	done := make(chan acp.PromptResponse, 1)

	go func() {
		response, _ := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "wait"}}}, &fakeNotifier{})
		done <- response
	}()
	fake.waitQuery(t)
	_, err := service.Prompt(context.Background(), acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: "text", Text: "second"}}}, &fakeNotifier{})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
	close(fake.block)
	<-done
}

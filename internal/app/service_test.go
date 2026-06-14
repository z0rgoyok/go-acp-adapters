package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-acp-adapter/internal/acp"
	"claude-acp-adapter/internal/claude"
)

func TestToolConfigDefaultsInNewSession(t *testing.T) {
	service := NewService(Options{Factory: fakeFactory(nil)})
	sessionID := mustSession(t, service)
	session, ok := service.registry.Get(sessionID)
	if !ok {
		t.Fatal("session not found")
	}
	if session.Config.ToolEvents != ToolEventsCompact {
		t.Fatalf("ToolEvents = %q", session.Config.ToolEvents)
	}
	if session.Config.ToolInputMaxBytes != 4096 {
		t.Fatalf("ToolInputMaxBytes = %d", session.Config.ToolInputMaxBytes)
	}
	if session.Config.ToolResultMaxBytes != 8192 {
		t.Fatalf("ToolResultMaxBytes = %d", session.Config.ToolResultMaxBytes)
	}
}

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
	if len(response.ConfigOptions) != 4 {
		t.Fatalf("config options = %+v", response.ConfigOptions)
	}
	wantCategories := map[string]string{"model": "model", "effort": "thought_level", "mode": "mode", "toolEvents": "tool"}
	for _, option := range response.ConfigOptions {
		if wantCategories[option.ID] != option.Category || option.Type != "select" || len(option.Options) == 0 {
			t.Fatalf("option = %+v", option)
		}
	}
	if response.Modes == nil || response.Modes.CurrentModeID != "auto" || len(response.Modes.AvailableModes) != 1 || response.Modes.AvailableModes[0].ID != "auto" {
		t.Fatalf("modes = %+v", response.Modes)
	}
}

func TestNewSessionConfigOptionsAreSDKSafe(t *testing.T) {
	service := NewService(Options{Factory: fakeFactory(nil)})

	response, err := service.NewSession(context.Background(), acp.NewSessionRequest{Cwd: t.TempDir()})
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
	configOptions, ok := wire["configOptions"].([]any)
	if !ok {
		t.Fatalf("configOptions not found in response: %s", data)
	}
	for _, opt := range configOptions {
		o := opt.(map[string]any)
		if o["type"] != "select" {
			t.Fatalf("option id=%q has type=%q, want \"select\"", o["id"], o["type"])
		}
		if _, ok := o["currentValue"].(string); !ok {
			t.Fatalf("option id=%q currentValue=%T is not a string", o["id"], o["currentValue"])
		}
		opts, ok := o["options"].([]any)
		if !ok || len(opts) == 0 {
			t.Fatalf("option id=%q options is empty or not an array", o["id"])
		}
		forbiddenIDs := map[string]bool{"toolInputMaxBytes": true, "toolResultMaxBytes": true}
		if _, ok := o["id"].(string); ok && forbiddenIDs[o["id"].(string)] {
			t.Fatalf("option id=%q must not be advertised in configOptions", o["id"])
		}
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
		{SessionID: sessionID, ConfigID: "model", Value: json.RawMessage(`"claude-sonnet-4-6"`)},
		{SessionID: sessionID, ConfigID: "effort", Value: json.RawMessage(`"high"`)},
		{SessionID: sessionID, ConfigID: "mode", Value: json.RawMessage(`"auto"`)},
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
	_, err := service.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{SessionID: sessionID, ConfigID: "effort", Value: json.RawMessage(`"high"`)})
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
	if len(notifier.updates) != 1 || notifier.updates[0].Update.SessionUpdate != "agent_message_chunk" || notifier.updates[0].Update.MessageID != "msg-1" {
		t.Fatalf("updates = %+v", notifier.updates)
	}
	var content struct{ Text string }
	if err := json.Unmarshal(notifier.updates[0].Update.Content, &content); err != nil || content.Text != "hello" {
		t.Fatalf("content = %+v", notifier.updates[0].Update.Content)
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
	if len(notifier.updates) != 4 {
		t.Fatalf("updates = %+v", notifier.updates)
	}
	if notifier.updates[0].Update.SessionUpdate != "agent_message_chunk" || notifier.updates[0].Update.MessageID != "msg-1" {
		t.Fatalf("update[0] = %+v", notifier.updates[0])
	}
	if notifier.updates[1].Update.SessionUpdate != "tool_call" || notifier.updates[1].Update.ToolCallID != "tool-1" || notifier.updates[1].Update.Kind != acp.ToolKindRead || notifier.updates[1].Update.Status != acp.ToolCallStatusPending {
		t.Fatalf("update[1] = %+v", notifier.updates[1])
	}
	if notifier.updates[2].Update.SessionUpdate != "tool_call_update" || notifier.updates[2].Update.ToolCallID != "tool-1" || notifier.updates[2].Update.Status != acp.ToolCallStatusCompleted {
		t.Fatalf("update[2] = %+v", notifier.updates[2])
	}
	if notifier.updates[3].Update.SessionUpdate != "agent_message_chunk" || notifier.updates[3].Update.MessageID != "msg-1" {
		t.Fatalf("update[3] = %+v", notifier.updates[3])
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

func TestNewServiceDefaultTimeoutIsZero(t *testing.T) {
	service := NewService(Options{Factory: fakeFactory(nil)})
	if service.timeout != 0 {
		t.Fatalf("NewService(Options{}).timeout = %v, want 0 (caller-owned)", service.timeout)
	}
}

func TestNewServicePropagatesTimeout(t *testing.T) {
	var captured time.Duration
	service := NewService(Options{
		Factory: func(opts TransportOptions) (Transport, error) {
			captured = opts.Timeout
			return &fakeTransport{}, nil
		},
		Timeout: 5 * time.Minute,
	})
	session, ok := service.registry.Get(mustSession(t, service))
	if !ok {
		t.Fatal("session not found")
	}
	_ = session
	if captured != 5*time.Minute {
		t.Fatalf("TransportOptions.Timeout = %v, want 5m", captured)
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

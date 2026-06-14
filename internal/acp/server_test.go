package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestServerInitialize(t *testing.T) {
	in := strings.NewReader(`{"id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	out := &bytes.Buffer{}
	server := NewServer(&fakeBackend{}, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("error = %+v", response.Error)
	}
}

func TestServerCancelNotificationWritesNoResponse(t *testing.T) {
	in := strings.NewReader(`{"method":"session/cancel","params":{"sessionId":"s1"}}` + "\n")
	out := &bytes.Buffer{}
	backend := &fakeBackend{}
	server := NewServer(backend, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
	if !backend.cancelled {
		t.Fatal("cancel was not called")
	}
}

func TestServerRejectsFractionalIDAsInvalidRequest(t *testing.T) {
	in := strings.NewReader(`{"id":1.5,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	out := &bytes.Buffer{}
	server := NewServer(&fakeBackend{}, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != CodeInvalidRequest {
		t.Fatalf("response = %+v", response)
	}
}

func TestServerRejectsMalformedJSONAsParseError(t *testing.T) {
	in := strings.NewReader(`{"id":1,"method":"initialize"` + "\n")
	out := &bytes.Buffer{}
	server := NewServer(&fakeBackend{}, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != CodeParseError || string(response.ID.Raw) != "null" {
		t.Fatalf("response = %+v", response)
	}
}

func TestServerRejectsUnknownMethod(t *testing.T) {
	in := strings.NewReader(`{"id":1,"method":"missing"}` + "\n")
	out := &bytes.Buffer{}
	server := NewServer(&fakeBackend{}, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != CodeMethodNotFound {
		t.Fatalf("response = %+v", response)
	}
}

func TestServerRejectsUnsupportedProtocolVersion(t *testing.T) {
	in := strings.NewReader(`{"id":1,"method":"initialize","params":{"protocolVersion":2}}` + "\n")
	out := &bytes.Buffer{}
	server := NewServer(&fakeBackend{}, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != CodeInvalidParams {
		t.Fatalf("response = %+v", response)
	}
}

func TestServerPromptWritesUpdateBeforeResponse(t *testing.T) {
	in := strings.NewReader(`{"id":"p1","method":"session/prompt","params":{"sessionId":"s1","prompt":[{"type":"text","text":"hi"}]}}` + "\n")
	out := &bytes.Buffer{}
	server := NewServer(&fakeBackend{promptText: "hello", messageID: "msg-1"}, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", out.String())
	}
	if !strings.Contains(lines[0], "session/update") || !strings.Contains(lines[1], "stopReason") {
		t.Fatalf("stdout = %q", out.String())
	}
	if !strings.Contains(lines[0], `"sessionUpdate":"agent_message_chunk"`) || !strings.Contains(lines[0], `"messageId":"msg-1"`) || !strings.Contains(lines[0], `"content":{"type":"text","text":"hello"}`) {
		t.Fatalf("update = %q", lines[0])
	}
}

func TestServerDecodesSchemaShapedStdioMCPServer(t *testing.T) {
	in := strings.NewReader(`{"id":"s1","method":"session/new","params":{"cwd":"/tmp","mcpServers":[{"name":"fs","type":"stdio","command":"node","args":["server.js"],"env":[{"name":"ROOT","value":"/tmp"}]}]}}` + "\n")
	out := &bytes.Buffer{}
	backend := &fakeBackend{}
	server := NewServer(backend, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(backend.mcpServers) != 1 || backend.mcpServers[0].Type != "stdio" || backend.mcpServers[0].Env[0].Name != "ROOT" {
		t.Fatalf("mcp servers = %+v", backend.mcpServers)
	}
}

func TestServerDispatchesSetSessionModel(t *testing.T) {
	in := strings.NewReader(`{"id":"m1","method":"session/set_model","params":{"sessionId":"s1","modelId":"claude-opus-4-8"}}` + "\n")
	out := &bytes.Buffer{}
	backend := &fakeBackend{}
	server := NewServer(backend, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || backend.modelID != "claude-opus-4-8" {
		t.Fatalf("response = %+v, modelID = %q", response, backend.modelID)
	}
}

func TestServerDispatchesSetSessionConfigOption(t *testing.T) {
	in := strings.NewReader(`{"id":"c1","method":"session/set_config_option","params":{"sessionId":"s1","configId":"effort","value":"high"}}` + "\n")
	out := &bytes.Buffer{}
	backend := &fakeBackend{}
	server := NewServer(backend, in, out, &bytes.Buffer{})

	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || backend.configID != "effort" || string(backend.configValue) != `"high"` {
		t.Fatalf("response = %+v, config = %q/%s", response, backend.configID, backend.configValue)
	}
}

func TestServerShutdownsWhenContextCancelledWhileIdle(t *testing.T) {
	in, writer := io.Pipe()
	defer writer.Close()
	backend := &blockingBackend{shutdownCalled: make(chan struct{})}
	server := NewServer(backend, in, &bytes.Buffer{}, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- server.Serve(ctx) }()
	cancel()
	waitFor(t, backend.shutdownCalled, "shutdown")
	if err := <-done; err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
}

type fakeBackend struct {
	cancelled   bool
	promptText  string
	messageID   string
	mcpServers  []McpServer
	modelID     string
	configID    string
	configValue json.RawMessage
}

func (f *fakeBackend) Initialize(_ context.Context, request InitializeRequest) (InitializeResponse, error) {
	if request.ProtocolVersion != ProtocolVersion {
		return InitializeResponse{}, testRPCError{code: CodeInvalidParams, message: "unsupported protocol version"}
	}
	return InitializeResponse{ProtocolVersion: ProtocolVersion, AgentInfo: &Implementation{Name: "test", Version: "dev"}}, nil
}

func (f *fakeBackend) NewSession(_ context.Context, request NewSessionRequest) (NewSessionResponse, error) {
	f.mcpServers = append([]McpServer(nil), request.McpServers...)
	return NewSessionResponse{SessionID: "s1"}, nil
}

func (f *fakeBackend) SetSessionModel(_ context.Context, request SetSessionModelRequest) (SetSessionModelResponse, error) {
	f.modelID = request.ModelID
	return SetSessionModelResponse{}, nil
}

func (f *fakeBackend) SetSessionConfigOption(_ context.Context, request SetSessionConfigOptionRequest) (SetSessionConfigOptionResponse, error) {
	f.configID = request.ConfigID
	f.configValue = request.Value
	return SetSessionConfigOptionResponse{}, nil
}

func (f *fakeBackend) Prompt(_ context.Context, request PromptRequest, notifier Notifier) (PromptResponse, error) {
	if f.promptText != "" {
		content, _ := json.Marshal(ContentBlock{Type: "text", Text: f.promptText})
		_ = notifier.SessionUpdate(SessionUpdateParams{SessionID: request.SessionID, Update: SessionUpdate{SessionUpdate: "agent_message_chunk", MessageID: f.messageID, Content: content}})
	}
	return PromptResponse{StopReason: "end_turn"}, nil
}

func (f *fakeBackend) CancelSession(context.Context, CancelSessionRequest) error {
	f.cancelled = true
	return nil
}

func (f *fakeBackend) CloseSession(context.Context, CloseSessionRequest) error { return nil }

func (f *fakeBackend) Shutdown(context.Context) error { return nil }

func waitFor(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

type testRPCError struct {
	code    int
	message string
}

func (e testRPCError) Error() string { return e.message }

func (e testRPCError) RPCCode() int { return e.code }

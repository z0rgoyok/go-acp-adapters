package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-acp-adapter/internal/app"
	"claude-acp-adapter/internal/claude"
)

func TestDefaultModeIsACPStdio(t *testing.T) {
	in := strings.NewReader(`{"id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	out := &bytes.Buffer{}
	errout := &bytes.Buffer{}

	if err := run(nil, in, out, errout); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("stdout is not JSON: %q", out.String())
	}
	if response["result"] == nil {
		t.Fatalf("response = %+v", response)
	}
}

func TestQueryModeRejectsEmptyPrompt(t *testing.T) {
	err := run([]string{"query"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestACPModeRunsBaselineLifecycleWithFakeTransport(t *testing.T) {
	fake := &cmdFakeTransport{response: claude.Response{Text: "hello", Messages: []claude.AssistantMessage{{Text: "hello", StopReason: "end_turn"}}}}
	client := startACP(t, fake)
	defer client.close(t)

	client.write(t, `{"id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	readResult(t, client.read(t), "result")
	client.write(t, fmt.Sprintf(`{"id":2,"method":"session/new","params":{"cwd":%q}}`, t.TempDir()))
	sessionID := readSessionID(t, client.read(t))
	client.write(t, fmt.Sprintf(`{"id":3,"method":"session/prompt","params":{"sessionId":%q,"prompt":[{"type":"text","text":"hi"}]}}`, sessionID))
	readMethod(t, client.read(t), "session/update")
	readStopReason(t, client.read(t), "end_turn")
	client.write(t, fmt.Sprintf(`{"id":4,"method":"session/close","params":{"sessionId":%q}}`, sessionID))
	readResult(t, client.read(t), "result")
	if !fake.isDisconnected() {
		t.Fatal("transport was not disconnected")
	}
}

func TestACPModeCancelsBackToBackPromptWithFakeTransport(t *testing.T) {
	fake := &cmdFakeTransport{block: make(chan struct{})}
	client := startACP(t, fake)
	defer client.close(t)

	client.write(t, `{"id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	readResult(t, client.read(t), "result")
	client.write(t, fmt.Sprintf(`{"id":2,"method":"session/new","params":{"cwd":%q}}`, t.TempDir()))
	sessionID := readSessionID(t, client.read(t))
	client.write(t, fmt.Sprintf(`{"id":3,"method":"session/prompt","params":{"sessionId":%q,"prompt":[{"type":"text","text":"wait"}]}}`, sessionID))
	client.write(t, fmt.Sprintf(`{"method":"session/cancel","params":{"sessionId":%q}}`, sessionID))
	readStopReason(t, client.read(t), "cancelled")
	if !fake.isCancelled() {
		t.Fatal("transport was not cancelled")
	}
}

func TestACPModeEOFDisconnectsOpenSessionWithFakeTransport(t *testing.T) {
	fake := &cmdFakeTransport{response: claude.Response{Text: "hello", Messages: []claude.AssistantMessage{{Text: "hello", StopReason: "end_turn"}}}}
	client := startACP(t, fake)

	client.write(t, `{"id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	readResult(t, client.read(t), "result")
	client.write(t, fmt.Sprintf(`{"id":2,"method":"session/new","params":{"cwd":%q}}`, t.TempDir()))
	readSessionID(t, client.read(t))
	client.close(t)
	if !fake.isDisconnected() {
		t.Fatal("transport was not disconnected on EOF")
	}
}

type acpClient struct {
	in      *io.PipeWriter
	out     *io.PipeReader
	scanner *bufio.Scanner
	done    chan error
}

func startACP(t *testing.T, transport *cmdFakeTransport) *acpClient {
	t.Helper()
	oldOptions := serviceOptions
	serviceOptions = app.Options{Factory: func(app.TransportOptions) (app.Transport, error) { return transport, nil }}
	t.Cleanup(func() { serviceOptions = oldOptions })
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(nil, inReader, outWriter, &bytes.Buffer{})
		_ = outWriter.Close()
		done <- err
	}()
	return &acpClient{in: inWriter, out: outReader, scanner: bufio.NewScanner(outReader), done: done}
}

func (c *acpClient) write(t *testing.T, line string) {
	t.Helper()
	if _, err := c.in.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

func (c *acpClient) read(t *testing.T) map[string]any {
	t.Helper()
	line := make(chan string, 1)
	go func() {
		if c.scanner.Scan() {
			line <- c.scanner.Text()
			return
		}
		line <- ""
	}()
	select {
	case text := <-line:
		if text == "" {
			t.Fatalf("stdout ended: %v", c.scanner.Err())
		}
		var message map[string]any
		if err := json.Unmarshal([]byte(text), &message); err != nil {
			t.Fatalf("stdout line is not JSON: %q", text)
		}
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stdout")
	}
	return nil
}

func (c *acpClient) close(t *testing.T) {
	t.Helper()
	_ = c.in.Close()
	select {
	case err := <-c.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ACP server")
	}
}

func readResult(t *testing.T, message map[string]any, key string) map[string]any {
	t.Helper()
	result, ok := message[key].(map[string]any)
	if !ok {
		t.Fatalf("message = %+v", message)
	}
	return result
}

func readMethod(t *testing.T, message map[string]any, method string) {
	t.Helper()
	if message["method"] != method {
		t.Fatalf("message = %+v", message)
	}
}

func readSessionID(t *testing.T, message map[string]any) string {
	t.Helper()
	result := readResult(t, message, "result")
	sessionID, ok := result["sessionId"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("message = %+v", message)
	}
	return sessionID
}

func readStopReason(t *testing.T, message map[string]any, want string) {
	t.Helper()
	result := readResult(t, message, "result")
	if result["stopReason"] != want {
		t.Fatalf("message = %+v", message)
	}
}

type cmdFakeTransport struct {
	mu           sync.Mutex
	response     claude.Response
	cancelled    bool
	disconnected bool
	block        chan struct{}
	streamEvents []claude.TranscriptEvent
}

func (f *cmdFakeTransport) Connect(context.Context) error { return nil }

func (f *cmdFakeTransport) Query(ctx context.Context, _ string) (claude.Response, error) {
	if f.block != nil {
		select {
		case <-ctx.Done():
			return claude.Response{}, ctx.Err()
		case <-f.block:
		}
	}
	return f.response, nil
}

func (f *cmdFakeTransport) StartTurn(ctx context.Context, prompt string) claude.TurnStream {
	events := make(chan claude.TranscriptEvent, 8)
	done := make(chan claude.TurnResult, 1)
	go func() {
		defer close(events)
		response, err := f.Query(ctx, prompt)
		if f.streamEvents != nil {
			for _, event := range f.streamEvents {
				select {
				case events <- event:
				case <-ctx.Done():
					done <- claude.TurnResult{Response: response, Err: ctx.Err()}
					close(done)
					return
				}
			}
		} else {
			for _, message := range response.Messages {
				if strings.TrimSpace(message.Text) == "" {
					continue
				}
				select {
				case events <- claude.AssistantTextEvent{MessageID: message.MessageID, Text: message.Text, StopReason: message.StopReason}:
				case <-ctx.Done():
					done <- claude.TurnResult{Response: response, Err: ctx.Err()}
					close(done)
					return
				}
			}
		}
		done <- claude.TurnResult{Response: response, Err: err}
		close(done)
	}()
	return claude.TurnStream{Events: events, Done: done}
}

func (f *cmdFakeTransport) Cancel(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
	return nil
}

func (f *cmdFakeTransport) Disconnect(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnected = true
	if path := os.Getenv("CLAUDE_ACP_ADAPTER_TEST_DISCONNECT_FILE"); path != "" {
		_ = os.WriteFile(path, []byte("disconnected"), 0o600)
	}
}

func (f *cmdFakeTransport) SessionID() string { return "claude-session" }

func (f *cmdFakeTransport) isCancelled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelled
}

func (f *cmdFakeTransport) isDisconnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disconnected
}

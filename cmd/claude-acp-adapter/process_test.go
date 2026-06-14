package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"claude-acp-adapter/internal/app"
	"claude-acp-adapter/internal/claude"
)

func TestACPProcessRunsBaselineLifecycleWithFakeTransport(t *testing.T) {
	client := startACPProcess(t, "lifecycle")
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
}

func TestACPProcessAcceptsSessionModelConfigurationWithFakeTransport(t *testing.T) {
	client := startACPProcess(t, "lifecycle")
	defer client.close(t)

	client.write(t, `{"id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	readResult(t, client.read(t), "result")
	client.write(t, fmt.Sprintf(`{"id":2,"method":"session/new","params":{"cwd":%q}}`, t.TempDir()))
	sessionID := readSessionID(t, client.read(t))
	client.write(t, fmt.Sprintf(`{"id":3,"method":"session/set_model","params":{"sessionId":%q,"modelId":"claude-opus-4-8"}}`, sessionID))
	readResult(t, client.read(t), "result")
}

func TestACPProcessCancelsBackToBackPromptWithFakeTransport(t *testing.T) {
	client := startACPProcess(t, "cancel")
	defer client.close(t)

	client.write(t, `{"id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	readResult(t, client.read(t), "result")
	client.write(t, fmt.Sprintf(`{"id":2,"method":"session/new","params":{"cwd":%q}}`, t.TempDir()))
	sessionID := readSessionID(t, client.read(t))
	client.write(t, fmt.Sprintf(`{"id":3,"method":"session/prompt","params":{"sessionId":%q,"prompt":[{"type":"text","text":"wait"}]}}`, sessionID))
	client.write(t, fmt.Sprintf(`{"method":"session/cancel","params":{"sessionId":%q}}`, sessionID))
	readStopReason(t, client.read(t), "cancelled")
}

func TestACPProcessSIGTERMDisconnectsOpenSessionWithFakeTransport(t *testing.T) {
	disconnectFile := t.TempDir() + "/disconnected"
	t.Setenv("CLAUDE_ACP_ADAPTER_TEST_DISCONNECT_FILE", disconnectFile)
	client := startACPProcess(t, "lifecycle")

	client.write(t, `{"id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	readResult(t, client.read(t), "result")
	client.write(t, fmt.Sprintf(`{"id":2,"method":"session/new","params":{"cwd":%q}}`, t.TempDir()))
	readSessionID(t, client.read(t))
	client.signalAndWait(t, syscall.SIGTERM)

	if _, err := os.Stat(disconnectFile); err != nil {
		t.Fatalf("transport was not disconnected on SIGTERM: %v; stderr: %s", err, client.stderr.String())
	}
}

func TestACPSubprocessHelper(t *testing.T) {
	if os.Getenv("CLAUDE_ACP_ADAPTER_TEST_HELPER") != "1" {
		return
	}
	transport := &cmdFakeTransport{response: claude.Response{Text: "hello", Messages: []claude.AssistantMessage{{Text: "hello", StopReason: "end_turn"}}}}
	if os.Getenv("CLAUDE_ACP_ADAPTER_TEST_MODE") == "cancel" {
		transport = &cmdFakeTransport{block: make(chan struct{})}
	}
	serviceOptions = app.Options{Factory: func(app.TransportOptions) (app.Transport, error) { return transport, nil }}
	if err := run(nil, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type acpProcessClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *bytes.Buffer
}

func startACPProcess(t *testing.T, mode string) *acpProcessClient {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestACPSubprocessHelper")
	cmd.Env = append(os.Environ(), "CLAUDE_ACP_ADAPTER_TEST_HELPER=1", "CLAUDE_ACP_ADAPTER_TEST_MODE="+mode)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return &acpProcessClient{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), stderr: stderr}
}

func (c *acpProcessClient) write(t *testing.T, line string) {
	t.Helper()
	if _, err := c.stdin.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

func (c *acpProcessClient) read(t *testing.T) map[string]any {
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
			t.Fatalf("stdout ended: %v; stderr: %s", c.scanner.Err(), c.stderr.String())
		}
		var message map[string]any
		if err := json.Unmarshal([]byte(text), &message); err != nil {
			t.Fatalf("stdout line is not JSON: %q", text)
		}
		return message
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for stdout; stderr: %s", c.stderr.String())
	}
	return nil
}

func (c *acpProcessClient) close(t *testing.T) {
	t.Helper()
	if err := c.stdin.Close(); err != nil && !strings.Contains(err.Error(), "file already closed") {
		t.Fatal(err)
	}
	if err := c.cmd.Wait(); err != nil {
		t.Fatalf("process failed: %v; stderr: %s", err, c.stderr.String())
	}
}

func (c *acpProcessClient) signalAndWait(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := c.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(time.Second):
		_ = c.cmd.Process.Kill()
		t.Fatalf("timed out waiting for process after signal; stderr: %s", c.stderr.String())
	}
}

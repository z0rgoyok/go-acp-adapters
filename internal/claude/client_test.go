package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettingsJSONContainsStopHook(t *testing.T) {
	client := &Client{fifoPath: "/tmp/path with spaces/test.stop", turnPath: "/tmp/path with spaces/test.turn"}

	var settings map[string]any
	if err := json.Unmarshal([]byte(client.settingsJSON()), &settings); err != nil {
		t.Fatal(err)
	}

	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	command := stop[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)

	if !strings.Contains(command, "cat ") || !strings.Contains(command, "turn_id") {
		t.Fatalf("command = %q", command)
	}
	if !strings.Contains(command, "'/tmp/path with spaces/test.stop'") {
		t.Fatalf("unquoted fifo path in command: %q", command)
	}
	if !strings.Contains(command, "'/tmp/path with spaces/test.turn'") {
		t.Fatalf("unquoted turn path in command: %q", command)
	}
}

func TestStopHookCommandAddsCurrentTurnID(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "stop.out")
	turnPath := filepath.Join(dir, "current.turn")
	if err := os.WriteFile(turnPath, []byte("turn-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &Client{fifoPath: fifoPath, turnPath: turnPath}
	cmd := exec.Command("sh", "-c", client.stopHookCommand())
	cmd.Stdin = strings.NewReader(`{"session_id":"s1","transcript_path":"/tmp/s1.jsonl"}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stop hook command failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(fifoPath)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := parseStopPayload(data)
	if !ok {
		t.Fatalf("payload was not parsed: %s", data)
	}
	if !currentTurnStop(payload, turnContext{ID: "turn-1"}) {
		t.Fatalf("payload did not match current turn: %+v", payload)
	}
	if payload.SessionID != "s1" || payload.TranscriptPath != "/tmp/s1.jsonl" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestCreateFIFO(t *testing.T) {
	path := t.TempDir() + "/stop.fifo"
	if err := createFIFO(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("%s is not a fifo: %s", path, info.Mode())
	}
}

func TestStopReaderReadsPayload(t *testing.T) {
	path := t.TempDir() + "/stop.fifo"
	if err := createFIFO(path); err != nil {
		t.Fatal(err)
	}

	reader, err := startStopReader(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Stop()

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"turn_id":"turn-1","session_id":"s1","transcript_path":"/tmp/s1.jsonl"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case payload := <-reader.Payloads():
		if payload.TurnID != "turn-1" || payload.SessionID != "s1" || payload.TranscriptPath != "/tmp/s1.jsonl" {
			t.Fatalf("payload = %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stop payload")
	}
}

func TestParseStopPayloadEnvelope(t *testing.T) {
	payload, ok := parseStopPayload([]byte(`{"turn_id":"turn-1","raw":{"session_id":"s1","transcript_path":"/tmp/s1.jsonl"}}`))
	if !ok {
		t.Fatal("payload was not parsed")
	}
	if payload.TurnID != "turn-1" || payload.SessionID != "s1" || payload.TranscriptPath != "/tmp/s1.jsonl" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestWaitForAssistantMessagesReturnsTimeoutWithoutTerminalAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"partial"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	stopReader := &stopReader{payloads: make(chan stopPayload)}
	reader := TranscriptReader{}
	turn := turnContext{ID: "turn-1", SessionID: "s1", TranscriptPath: path}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := waitForAssistantMessages(ctx, stopReader, &reader, path, turn, nil)
	if err == nil {
		t.Fatal("expected timeout without terminal assistant stop reason")
	}
}

func TestWaitForAssistantMessagesReturnsOnTerminalStopReasons(t *testing.T) {
	for _, reason := range []string{"max_turn_requests", "refusal"} {
		t.Run(reason, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			line := `{"type":"assistant","message":{"role":"assistant","stop_reason":"` + reason + `","content":[{"type":"text","text":"done"}]}}` + "\n"
			if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}

			stopReader := &stopReader{payloads: make(chan stopPayload)}
			reader := TranscriptReader{}
			turn := turnContext{ID: "turn-1", SessionID: "s1", TranscriptPath: path}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan []AssistantMessage, 1)
			errs := make(chan error, 1)
			go func() {
				messages, err := waitForAssistantMessages(ctx, stopReader, &reader, path, turn, nil)
				if err != nil {
					errs <- err
					return
				}
				done <- messages
			}()

			select {
			case err := <-errs:
				t.Fatal(err)
			case messages := <-done:
				if len(messages) != 1 || messages[0].Text != "done" || messages[0].StopReason != reason {
					t.Fatalf("messages = %+v", messages)
				}
			case <-time.After(100 * time.Millisecond):
				cancel()
				t.Fatal("timed out waiting for terminal assistant message")
			}
		})
	}
}

func TestWaitForAssistantMessagesIgnoresStaleStopUntilTerminalAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	firstLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"partial"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(firstLine), 0o600); err != nil {
		t.Fatal(err)
	}

	stopReader := &stopReader{payloads: make(chan stopPayload, 1)}
	reader := TranscriptReader{}
	turn := turnContext{ID: "turn-1", SessionID: "s1", TranscriptPath: path}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan []AssistantMessage, 1)
	errs := make(chan error, 1)
	go func() {
		messages, err := waitForAssistantMessages(ctx, stopReader, &reader, path, turn, nil)
		if err != nil {
			errs <- err
			return
		}
		done <- messages
	}()
	stopReader.payloads <- stopPayload{TranscriptPath: path}

	time.Sleep(50 * time.Millisecond)
	finalLine := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}` + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(finalLine); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Fatal(err)
	case messages := <-done:
		if len(messages) != 2 {
			t.Fatalf("messages = %+v", messages)
		}
		if messages[0].Text != "partial" || messages[1].Text != "done" || messages[1].StopReason != "end_turn" {
			t.Fatalf("messages = %+v", messages)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for final assistant message")
	}
}

func TestWaitForAssistantMessagesDoesNotFinishOnToolUseStopReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	toolUse := `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"tool-1","name":"Read","input":{}}]}}` + "\n"
	if err := os.WriteFile(path, []byte(toolUse), 0o600); err != nil {
		t.Fatal(err)
	}

	stopReader := &stopReader{payloads: make(chan stopPayload)}
	reader := TranscriptReader{}
	turn := turnContext{ID: "turn-1", SessionID: "s1", TranscriptPath: path}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan []AssistantMessage, 1)
	errs := make(chan error, 1)
	go func() {
		messages, err := waitForAssistantMessages(ctx, stopReader, &reader, path, turn, nil)
		if err != nil {
			errs <- err
			return
		}
		done <- messages
	}()

	select {
	case messages := <-done:
		t.Fatalf("completed on tool_use: %+v", messages)
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(50 * time.Millisecond):
	}
	finalLine := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}` + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(finalLine); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Fatal(err)
	case messages := <-done:
		if len(messages) != 1 || messages[0].Text != "done" || messages[0].StopReason != "end_turn" {
			t.Fatalf("messages = %+v", messages)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for terminal assistant message")
	}
}

func TestWaitForAssistantMessagesFiltersBeforeTurnOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	oldLine := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"old"}]}}` + "\n"
	newLine := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"new"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(oldLine+newLine), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := TranscriptReader{}
	turn := turnContext{ID: "turn-1", SessionID: "s1", TranscriptPath: path, StartOffset: int64(len(oldLine))}
	messages, err := waitForAssistantMessages(context.Background(), &stopReader{payloads: make(chan stopPayload)}, &reader, path, turn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Text != "new" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestNewTurnContextStartsAfterStaleTranscriptBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	staleLine := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"stale"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(staleLine), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := TranscriptReader{}
	turn := newTurnContext("s1", &reader, path)
	if turn.StartOffset != int64(len(staleLine)) {
		t.Fatalf("StartOffset = %d, want %d", turn.StartOffset, len(staleLine))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan []AssistantMessage, 1)
	errs := make(chan error, 1)
	go func() {
		messages, err := waitForAssistantMessages(ctx, &stopReader{payloads: make(chan stopPayload)}, &reader, path, turn, nil)
		if err != nil {
			errs <- err
			return
		}
		done <- messages
	}()

	select {
	case messages := <-done:
		t.Fatalf("completed on stale transcript bytes: %+v", messages)
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(50 * time.Millisecond):
	}

	newLine := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"new"}]}}` + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(newLine); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Fatal(err)
	case messages := <-done:
		if len(messages) != 1 || messages[0].Text != "new" {
			t.Fatalf("messages = %+v", messages)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for new terminal assistant message")
	}
}

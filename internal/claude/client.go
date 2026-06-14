package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Client struct {
	options    Options
	sessionID  string
	fifoPath   string
	turnPath   string
	markerPath string
	ownership  OwnerMarker
	tmux       *TmuxSession
	stopReader *stopReader
	transcript TranscriptReader
	mu         sync.Mutex
	queryMu    sync.Mutex
}

func NewClient(options Options) (*Client, error) {
	if options.ClaudePath == "" {
		options.ClaudePath = "claude"
	}
	if options.WorkingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		options.WorkingDir = cwd
	}
	if options.PermissionMode == "" {
		options.PermissionMode = "bypassPermissions"
	}
	if options.Timeout == 0 {
		options.Timeout = 90 * time.Second
	}
	return &Client{options: options}, nil
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tmux != nil {
		return fmt.Errorf("client is already connected")
	}
	if err := requireExecutable("tmux"); err != nil {
		return fmt.Errorf("tmux executable: %w", err)
	}
	if err := requireExecutable(c.options.ClaudePath); err != nil {
		return fmt.Errorf("claude executable: %w", err)
	}

	sessionID := uuid.NewString()
	c.sessionID = sessionID
	c.fifoPath = filepath.Join(os.TempDir(), "claude-acp-adapter-"+sessionID+".stop")
	c.turnPath = filepath.Join(os.TempDir(), "claude-acp-adapter-"+sessionID+".turn")
	if err := createFIFO(c.fifoPath); err != nil {
		return err
	}
	stopReader, err := startStopReader(ctx, c.fifoPath)
	if err != nil {
		_ = os.Remove(c.fifoPath)
		return err
	}

	name := c.options.TmuxName
	if name == "" {
		name = "claude-" + sessionID[:8]
	}
	marker, markerPath, err := newOwnerMarker(name, sessionID, c.options.WorkingDir, c.fifoPath)
	if err != nil {
		stopReader.Stop()
		_ = os.Remove(c.fifoPath)
		return err
	}
	env := runtimeEnv(c.options.ConfigDir)
	for key, value := range marker.Env() {
		env[key] = value
	}
	c.tmux = &TmuxSession{
		Name:       name,
		WorkingDir: c.options.WorkingDir,
		ClaudePath: c.options.ClaudePath,
		Env:        env,
	}
	if err := c.tmux.Launch(ctx, c.claudeArgs()); err != nil {
		stopReader.Stop()
		_ = os.Remove(c.fifoPath)
		removeOwnerMarker(markerPath)
		c.tmux = nil
		return err
	}
	c.stopReader = stopReader
	c.markerPath = markerPath
	c.ownership = marker
	registerActiveSession(activeSession{tmuxName: name, fifoPath: c.fifoPath, markerPath: markerPath, claudeSessionID: sessionID, cwd: c.options.WorkingDir})
	return nil
}

func (c *Client) Query(ctx context.Context, prompt string) (response Response, err error) {
	stream := c.StartTurn(ctx, prompt)
	for range stream.Events {
	}
	result := <-stream.Done
	return result.Response, result.Err
}

func (c *Client) StartTurn(ctx context.Context, prompt string) TurnStream {
	events := make(chan TranscriptEvent, 32)
	done := make(chan TurnResult, 1)
	go func() {
		defer close(events)
		response, err := c.runTurn(ctx, prompt, events)
		done <- TurnResult{Response: response, Err: err}
		close(done)
	}()
	return TurnStream{Events: events, Done: done}
}

func (c *Client) runTurn(ctx context.Context, prompt string, events chan<- TranscriptEvent) (response Response, err error) {
	c.queryMu.Lock()
	defer c.queryMu.Unlock()

	c.mu.Lock()
	if c.tmux == nil {
		c.mu.Unlock()
		return Response{}, fmt.Errorf("client is not connected")
	}
	tmux := c.tmux
	stopReader := c.stopReader
	sessionID := c.sessionID
	timeout := c.options.Timeout
	configDir := c.options.ConfigDir
	markerPath := c.markerPath
	turnPath := c.turnPath
	transcript := &c.transcript
	c.mu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	promptSent := false
	defer func() {
		if err != nil && promptSent && queryCtx.Err() != nil {
			_ = tmux.Interrupt(context.Background())
		}
	}()

	start := time.Now()
	if err := tmux.WaitReady(queryCtx); err != nil {
		return Response{}, err
	}
	path := findTranscriptPath(transcriptRoots(configDir), sessionID)
	turn := newTurnContext(sessionID, transcript, path)
	stopReader.Drain()
	if err := writeCurrentTurn(turnPath, turn); err != nil {
		return Response{}, err
	}
	if err := tmux.PastePrompt(queryCtx, sessionID, prompt); err != nil {
		return Response{}, err
	}
	promptSent = true

	path, err = waitForTranscript(queryCtx, stopReader, configDir, sessionID, turn)
	if err != nil {
		return Response{}, err
	}
	turn.TranscriptPath = path
	messages, err := waitForAssistantMessages(queryCtx, stopReader, transcript, path, turn, events)
	if err != nil {
		return Response{}, err
	}
	return Response{
		SessionID:      sessionID,
		TurnID:         turn.ID,
		Text:           joinAssistantText(messages),
		Messages:       messages,
		Duration:       time.Since(start),
		TranscriptPath: path,
		Diagnostics: TurnDiagnostics{
			SessionID:       sessionID,
			ClaudeSessionID: sessionID,
			TurnID:          turn.ID,
			TmuxName:        tmux.Name,
			TranscriptPath:  path,
			StartOffset:     turn.StartOffset,
			OwnershipMarker: markerPath,
		},
	}, nil
}

func (c *Client) Cancel(ctx context.Context) error {
	c.mu.Lock()
	tmux := c.tmux
	c.mu.Unlock()
	if tmux == nil {
		return fmt.Errorf("client is not connected")
	}
	return tmux.Interrupt(ctx)
}

func (c *Client) Disconnect(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopReader != nil {
		c.stopReader.Stop()
		c.stopReader = nil
	}
	if c.tmux != nil {
		session := activeSession{tmuxName: c.tmux.Name, fifoPath: c.fifoPath, markerPath: c.markerPath, claudeSessionID: c.sessionID, cwd: c.options.WorkingDir}
		if !cleanupActiveSession(ctx, session, killTmuxSession, tmuxSessionAlive) {
			return
		}
		c.tmux = nil
	}
	if c.fifoPath != "" {
		_ = os.Remove(c.fifoPath)
		c.fifoPath = ""
	}
	if c.turnPath != "" {
		_ = os.Remove(c.turnPath)
		c.turnPath = ""
	}
	removeOwnerMarker(c.markerPath)
	c.markerPath = ""
}

func (c *Client) SessionID() string {
	return c.sessionID
}

func (c *Client) claudeArgs() []string {
	args := []string{
		"--session-id", c.sessionID,
		"--permission-mode", c.options.PermissionMode,
		"--settings", c.settingsJSON(),
	}
	if c.options.Model != "" {
		args = append(args, "--model", c.options.Model)
	}
	args = append(args, c.options.ExtraArgs...)
	return args
}

func (c *Client) settingsJSON() string {
	settings := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": c.stopHookCommand(),
						},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(settings)
	return string(data)
}

func (c *Client) stopHookCommand() string {
	return `sh -c 'turn=$(cat "$1" 2>/dev/null || true); printf '\''{"turn_id":"%s","raw":'\'' "$turn"; cat; printf '\''}\n'\''' claude-acp-stop ` + shellQuote(c.turnPath) + " >> " + shellQuote(c.fifoPath)
}

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"claude-acp-adapter/internal/acp"
)

func (s *Service) Initialize(_ context.Context, request acp.InitializeRequest) (acp.InitializeResponse, error) {
	if request.ProtocolVersion != acp.ProtocolVersion {
		return acp.InitializeResponse{}, invalidParams("unsupported protocol version")
	}
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion,
		AgentInfo:       &acp.Implementation{Name: "claude-acp-adapter", Version: "dev"},
		AgentCapabilities: acp.AgentCapabilities{
			PromptCapabilities:  acp.PromptCapabilities{Text: true, ResourceLink: true},
			McpCapabilities:     acp.McpCapabilities{Stdio: true},
			SessionCapabilities: acp.SessionCapabilities{Close: &acp.CloseSessionCapability{}},
		},
		AuthMethods: []acp.AuthMethod{},
	}, nil
}

func (s *Service) NewSession(ctx context.Context, request acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	if s.isClosing() {
		return acp.NewSessionResponse{}, invalidParams("service is shutting down")
	}
	if !filepath.IsAbs(request.Cwd) {
		return acp.NewSessionResponse{}, invalidParams("cwd must be an absolute path")
	}
	for _, dir := range request.AdditionalDirectories {
		if !filepath.IsAbs(dir) {
			return acp.NewSessionResponse{}, invalidParams("additionalDirectories must contain absolute paths")
		}
	}
	mcpPath, err := buildMCPConfig(request.McpServers)
	if err != nil {
		return acp.NewSessionResponse{}, err
	}
	extraArgs := []string(nil)
	if mcpPath != "" {
		extraArgs = append(extraArgs, "--mcp-config", mcpPath)
	}
	config := newSessionConfig(s.model, s.toolCfg)
	transport, err := s.factory(TransportOptions{WorkingDir: request.Cwd, Model: config.Model, Timeout: s.timeout, ExtraArgs: extraArgs})
	if err != nil {
		removeFile(mcpPath)
		return acp.NewSessionResponse{}, err
	}
	if err := transport.Connect(ctx); err != nil {
		removeFile(mcpPath)
		transport.Disconnect(context.Background())
		return acp.NewSessionResponse{}, internalError("Claude transport startup failure: " + err.Error())
	}
	now := time.Now()
	session := &Session{
		ID:                    newSessionID(),
		Cwd:                   request.Cwd,
		AdditionalDirectories: append([]string(nil), request.AdditionalDirectories...),
		MCPConfigPath:         mcpPath,
		Transport:             transport,
		Config:                config,
		ExtraArgs:             append([]string(nil), extraArgs...),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	s.registry.Add(session)
	return acp.NewSessionResponse{SessionID: session.ID, ConfigOptions: config.options(), Modes: config.modes()}, nil
}

func (s *Service) Prompt(ctx context.Context, request acp.PromptRequest, notifier acp.Notifier) (acp.PromptResponse, error) {
	turn, err := s.StartPrompt(ctx, request)
	if err != nil {
		return acp.PromptResponse{}, err
	}
	return turn.Run(ctx, notifier)
}

func (s *Service) StartPrompt(ctx context.Context, request acp.PromptRequest) (acp.PromptTurn, error) {
	if s.isClosing() {
		return nil, invalidParams("service is shutting down")
	}
	session, ok := s.registry.Get(request.SessionID)
	if !ok {
		return nil, invalidParams("unknown session")
	}
	prompt, err := promptText(request.Prompt)
	if err != nil {
		return nil, err
	}
	turnCtx, cancel := context.WithCancel(ctx)
	if !session.beginTurn(cancel) {
		cancel()
		return nil, invalidParams("prompt already running for the session")
	}
	return &PromptTurn{session: session, prompt: prompt, ctx: turnCtx, cancel: cancel}, nil
}

type PromptTurn struct {
	session *Session
	prompt  string
	ctx     context.Context
	cancel  context.CancelFunc
}

func (t *PromptTurn) Run(_ context.Context, notifier acp.Notifier) (acp.PromptResponse, error) {
	defer t.cancel()
	defer t.session.finishTurn()

	stream := t.session.Transport.StartTurn(t.ctx, t.prompt)
	for event := range stream.Events {
		update, ok := updateFromTranscriptEvent(event, t.session.Config)
		if !ok {
			continue
		}
		_ = notifier.SessionUpdate(acp.SessionUpdateParams{SessionID: t.session.ID, Update: update})
	}
	result := <-stream.Done
	response, err := result.Response, result.Err
	if cancelErr := t.session.waitCancelResult(); cancelErr != nil {
		return acp.PromptResponse{}, internalError("Claude transport cancellation failure: " + cancelErr.Error())
	}
	cancelled := t.session.isCancelling() || t.ctx.Err() != nil
	if err != nil && !cancelled {
		return acp.PromptResponse{}, internalError("Claude transport failure: " + err.Error())
	}
	reason, err := stopReason(response, cancelled)
	if err != nil {
		return acp.PromptResponse{}, err
	}
	return acp.PromptResponse{StopReason: reason}, nil
}

func (s *Service) CancelSession(ctx context.Context, request acp.CancelSessionRequest) error {
	session, ok := s.registry.Get(request.SessionID)
	if !ok {
		return nil
	}
	if cancel := session.startCancel(); cancel != nil {
		err := session.Transport.Cancel(ctx)
		session.completeCancel(err)
		cancel()
		if err != nil {
			return internalError("Claude transport cancellation failure: " + err.Error())
		}
	}
	return nil
}

func (s *Service) CloseSession(ctx context.Context, request acp.CloseSessionRequest) error {
	session, ok := s.registry.Delete(request.SessionID)
	if !ok {
		return invalidParams("unknown session")
	}
	session.cleanup(ctx)
	return nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.setClosing()
	for _, session := range s.registry.All() {
		s.registry.Delete(session.ID)
		session.cleanup(ctx)
	}
	return nil
}

func (s *Service) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

func (s *Service) setClosing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
}

func (s *Session) beginTurn(cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdatedAt = time.Now()
	if s.active != nil {
		return false
	}
	s.active = &activeTurn{cancel: cancel, cancelDone: make(chan struct{})}
	return true
}

func (s *Session) finishTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdatedAt = time.Now()
	s.active = nil
}

func (s *Session) startCancel() context.CancelFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil
	}
	s.active.cancelling = true
	return s.active.cancel
}

func (s *Session) completeCancel(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.active.cancelErr = err
		if s.active.cancelDone != nil {
			close(s.active.cancelDone)
			s.active.cancelDone = nil
		}
	}
}

func (s *Session) isCancelling() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active != nil && s.active.cancelling
}

func (s *Session) waitCancelResult() error {
	s.mu.Lock()
	if s.active == nil || !s.active.cancelling || s.active.cancelDone == nil {
		defer s.mu.Unlock()
		if s.active == nil {
			return nil
		}
		return s.active.cancelErr
	}
	done := s.active.cancelDone
	s.mu.Unlock()

	<-done

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil
	}
	return s.active.cancelErr
}

func (s *Session) cleanup(ctx context.Context) {
	if cancel := s.startCancel(); cancel != nil {
		err := s.Transport.Cancel(ctx)
		s.completeCancel(err)
		cancel()
	}
	s.Transport.Disconnect(ctx)
	removeFile(s.MCPConfigPath)
}

func removeFile(path string) {
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "remove %s: %v\n", path, err)
		}
	}
}

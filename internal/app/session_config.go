package app

import (
	"context"
	"strings"
	"time"

	"claude-acp-adapter/internal/acp"
)

const (
	configIDModel  = "model"
	configIDEffort = "effort"
	configIDMode   = "mode"

	defaultEffort = "medium"
	defaultMode   = "auto"
)

type SessionConfig struct {
	Model  string
	Effort string
	Mode   string
}

func newSessionConfig(model string) SessionConfig {
	return SessionConfig{Model: model, Effort: defaultEffort, Mode: defaultMode}
}

func (c SessionConfig) options() []acp.SessionConfigOption {
	return []acp.SessionConfigOption{
		selectOption(configIDModel, "Model", "model", c.Model, []string{"claude-opus-4-8", "claude-sonnet-4-6"}),
		selectOption(configIDEffort, "Reasoning effort", "thought_level", c.Effort, []string{"low", "medium", "high"}),
		selectOption(configIDMode, "Mode", "mode", c.Mode, []string{"auto"}),
	}
}

func (c SessionConfig) modes() *acp.SessionModeState {
	return &acp.SessionModeState{
		AvailableModes: []acp.SessionMode{{ID: defaultMode, Name: "Auto"}},
		CurrentModeID:  c.Mode,
	}
}

func selectOption(id, name, category, current string, values []string) acp.SessionConfigOption {
	options := make([]acp.SessionConfigSelectOption, 0, len(values))
	for _, value := range values {
		options = append(options, acp.SessionConfigSelectOption{Name: value, Value: value})
	}
	return acp.SessionConfigOption{Type: "select", ID: id, Name: name, Category: category, CurrentValue: current, Options: options}
}

func (s *Service) SetSessionModel(ctx context.Context, request acp.SetSessionModelRequest) (acp.SetSessionModelResponse, error) {
	model := strings.TrimSpace(request.ModelID)
	if model == "" {
		return acp.SetSessionModelResponse{}, invalidParams("modelId is required")
	}
	if err := s.updateSessionConfig(ctx, request.SessionID, configIDModel, model); err != nil {
		return acp.SetSessionModelResponse{}, err
	}
	return acp.SetSessionModelResponse{}, nil
}

func (s *Service) SetSessionConfigOption(ctx context.Context, request acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	configID := strings.TrimSpace(request.ConfigID)
	value := strings.TrimSpace(request.Value)
	if configID == "" {
		return acp.SetSessionConfigOptionResponse{}, invalidParams("configId is required")
	}
	if value == "" {
		return acp.SetSessionConfigOptionResponse{}, invalidParams("value is required")
	}
	if err := s.updateSessionConfig(ctx, request.SessionID, configID, value); err != nil {
		return acp.SetSessionConfigOptionResponse{}, err
	}
	return acp.SetSessionConfigOptionResponse{}, nil
}

func (s *Service) updateSessionConfig(ctx context.Context, sessionID, configID, value string) error {
	session, ok := s.registry.Get(sessionID)
	if !ok {
		return invalidParams("unknown session")
	}
	switch configID {
	case configIDModel:
		return s.setSessionModel(ctx, session, value)
	case configIDEffort:
		return session.setConfigValue(func(config *SessionConfig) { config.Effort = value })
	case configIDMode:
		return session.setConfigValue(func(config *SessionConfig) { config.Mode = value })
	default:
		return invalidParams("unsupported configId: " + configID)
	}
}

func (s *Service) setSessionModel(ctx context.Context, session *Session, model string) error {
	session.mu.Lock()
	if session.active != nil {
		session.mu.Unlock()
		return invalidParams("session config cannot be changed during an active prompt")
	}
	if session.Config.Model == model {
		session.UpdatedAt = time.Now()
		session.mu.Unlock()
		return nil
	}
	options := TransportOptions{WorkingDir: session.Cwd, Model: model, Timeout: s.timeout, ExtraArgs: append([]string(nil), session.ExtraArgs...)}
	session.mu.Unlock()

	transport, err := s.factory(options)
	if err != nil {
		return err
	}
	if err := transport.Connect(ctx); err != nil {
		transport.Disconnect(context.Background())
		return internalError("Claude transport startup failure: " + err.Error())
	}

	session.mu.Lock()
	if session.active != nil {
		session.mu.Unlock()
		transport.Disconnect(context.Background())
		return invalidParams("session config cannot be changed during an active prompt")
	}
	oldTransport := session.Transport
	session.Transport = transport
	session.Config.Model = model
	session.UpdatedAt = time.Now()
	session.mu.Unlock()

	oldTransport.Disconnect(context.Background())
	return nil
}

func (s *Session) setConfigValue(update func(*SessionConfig)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return invalidParams("session config cannot be changed during an active prompt")
	}
	update(&s.Config)
	s.UpdatedAt = time.Now()
	return nil
}

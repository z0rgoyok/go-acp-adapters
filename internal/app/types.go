package app

import (
	"context"
	"sync"
	"time"

	"claude-acp-adapter/internal/claude"
)

type Transport interface {
	Connect(context.Context) error
	Query(context.Context, string) (claude.Response, error)
	StartTurn(context.Context, string) claude.TurnStream
	Cancel(context.Context) error
	Disconnect(context.Context)
	SessionID() string
}

type TransportFactory func(TransportOptions) (Transport, error)

type TransportOptions struct {
	WorkingDir string
	Model      string
	Timeout    time.Duration
	ExtraArgs  []string
}

type Service struct {
	mu       sync.Mutex
	registry *Registry
	factory  TransportFactory
	model    string
	timeout  time.Duration
	toolCfg  ToolObservabilityConfig
	closing  bool
}

type Options struct {
	Factory TransportFactory
	Model   string
	Timeout time.Duration
	ToolCfg ToolObservabilityConfig
}

func NewService(options Options) *Service {
	factory := options.Factory
	if factory == nil {
		factory = NewClaudeTransport
	}
	if options.Timeout == 0 {
		options.Timeout = 90 * time.Second
	}
	toolCfg := options.ToolCfg
	if toolCfg.ToolEvents == "" {
		toolCfg = DefaultToolObservabilityConfig()
	}
	return &Service{
		registry: NewRegistry(),
		factory:  factory,
		model:    options.Model,
		timeout:  options.Timeout,
		toolCfg:  toolCfg,
	}
}

func NewClaudeTransport(options TransportOptions) (Transport, error) {
	return claude.NewClient(claude.Options{
		WorkingDir: options.WorkingDir,
		Model:      options.Model,
		Timeout:    options.Timeout,
		ExtraArgs:  options.ExtraArgs,
	})
}

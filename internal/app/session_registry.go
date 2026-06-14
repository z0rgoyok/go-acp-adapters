package app

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Registry struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

type Session struct {
	mu                    sync.Mutex
	ID                    string
	Cwd                   string
	AdditionalDirectories []string
	MCPConfigPath         string
	Transport             Transport
	Config                SessionConfig
	ExtraArgs             []string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	active                *activeTurn
}

type activeTurn struct {
	cancel     context.CancelFunc
	cancelling bool
	cancelErr  error
	cancelDone chan struct{}
}

func NewRegistry() *Registry {
	return &Registry{sessions: map[string]*Session{}}
}

func (r *Registry) Add(session *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = session
}

func (r *Registry) Get(id string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[id]
	return session, ok
}

func (r *Registry) Delete(id string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[id]
	if ok {
		delete(r.sessions, id)
	}
	return session, ok
}

func (r *Registry) All() []*Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

func newSessionID() string {
	return uuid.NewString()
}

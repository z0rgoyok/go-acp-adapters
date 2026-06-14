package claude

import (
	"context"
	"fmt"
	"os"
	"sync"
)

type activeSession struct {
	tmuxName        string
	fifoPath        string
	markerPath      string
	claudeSessionID string
	cwd             string
}

type tmuxKillFunc func(context.Context, string) error

type tmuxAliveFunc func(context.Context, string) bool

var activeSessions = struct {
	sync.Mutex
	items map[string]activeSession
}{
	items: map[string]activeSession{},
}

func registerActiveSession(session activeSession) {
	activeSessions.Lock()
	defer activeSessions.Unlock()
	activeSessions.items[session.tmuxName] = session
}

func unregisterActiveSession(tmuxName string) {
	activeSessions.Lock()
	defer activeSessions.Unlock()
	delete(activeSessions.items, tmuxName)
}

func CleanupActiveSessions(ctx context.Context) {
	for _, session := range activeSessionSnapshot() {
		cleanupActiveSession(ctx, session, killTmuxSession, tmuxSessionAlive)
	}
}

func cleanupActiveSession(ctx context.Context, session activeSession, kill tmuxKillFunc, alive tmuxAliveFunc) bool {
	if err := kill(ctx, session.tmuxName); err != nil {
		logCleanupError(session, err)
		if alive(ctx, session.tmuxName) {
			return false
		}
	}
	if session.fifoPath != "" {
		_ = os.Remove(session.fifoPath)
	}
	removeOwnerMarker(session.markerPath)
	unregisterActiveSession(session.tmuxName)
	return true
}

func killTmuxSession(ctx context.Context, name string) error {
	return (&TmuxSession{Name: name}).Kill(ctx)
}

func tmuxSessionAlive(ctx context.Context, name string) bool {
	return (&TmuxSession{Name: name}).IsAlive(ctx)
}

func logCleanupError(session activeSession, err error) {
	fmt.Fprintf(os.Stderr, "cleanup tmux=%q claudeSession=%q fifo=%q marker=%q: %v\n", session.tmuxName, session.claudeSessionID, session.fifoPath, session.markerPath, err)
}

func activeSessionSnapshot() []activeSession {
	activeSessions.Lock()
	defer activeSessions.Unlock()

	out := make([]activeSession, 0, len(activeSessions.items))
	for _, session := range activeSessions.items {
		out = append(out, session)
	}
	return out
}

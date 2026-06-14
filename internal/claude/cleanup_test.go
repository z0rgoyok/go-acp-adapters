package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveSessionRegistry(t *testing.T) {
	resetActiveSessions(t)

	registerActiveSession(activeSession{tmuxName: "claude-one", fifoPath: "/tmp/one.stop", markerPath: "/tmp/one.marker", claudeSessionID: "s1", cwd: "/work"})
	snapshot := activeSessionSnapshot()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot = %v", snapshot)
	}
	if snapshot[0].tmuxName != "claude-one" || snapshot[0].fifoPath != "/tmp/one.stop" || snapshot[0].markerPath != "/tmp/one.marker" || snapshot[0].claudeSessionID != "s1" {
		t.Fatalf("session = %+v", snapshot[0])
	}

	unregisterActiveSession("claude-one")
	if snapshot := activeSessionSnapshot(); len(snapshot) != 0 {
		t.Fatalf("snapshot after unregister = %v", snapshot)
	}
}

func TestCleanupActiveSessionPreservesEvidenceWhenKillFailsAndSessionLives(t *testing.T) {
	resetActiveSessions(t)
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "session.stop")
	markerPath := filepath.Join(dir, "session.marker")
	if err := os.WriteFile(fifoPath, []byte("fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := activeSession{tmuxName: "claude-live", fifoPath: fifoPath, markerPath: markerPath, claudeSessionID: "s1", cwd: "/work"}
	registerActiveSession(session)

	cleaned := cleanupActiveSession(context.Background(), session, func(context.Context, string) error {
		return errors.New("kill failed")
	}, func(context.Context, string) bool {
		return true
	})

	if cleaned {
		t.Fatal("cleanup reported success for a still-live session")
	}
	if _, err := os.Stat(fifoPath); err != nil {
		t.Fatalf("fifo was removed after failed cleanup: %v", err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker was removed after failed cleanup: %v", err)
	}
	if snapshot := activeSessionSnapshot(); len(snapshot) != 1 || snapshot[0].tmuxName != session.tmuxName {
		t.Fatalf("registry entry was not preserved: %+v", snapshot)
	}
}

func resetActiveSessions(t *testing.T) {
	t.Helper()
	activeSessions.Lock()
	activeSessions.items = map[string]activeSession{}
	activeSessions.Unlock()
	t.Cleanup(func() {
		activeSessions.Lock()
		activeSessions.items = map[string]activeSession{}
		activeSessions.Unlock()
	})
}

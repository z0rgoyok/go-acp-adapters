package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindStaleOwnedSessionsFindsOnlyOldOwnedMarkers(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	old := OwnerMarker{InstanceID: "old", PID: 1, TmuxName: "claude-old", CreatedAt: now.Add(-time.Hour)}
	fresh := OwnerMarker{InstanceID: "fresh", PID: 1, TmuxName: "claude-fresh", CreatedAt: now.Add(-time.Minute)}
	writeMarker(t, filepath.Join(dir, ownerMarkerPrefix+old.InstanceID+".json"), old)
	writeMarker(t, filepath.Join(dir, ownerMarkerPrefix+fresh.InstanceID+".json"), fresh)
	writeMarker(t, filepath.Join(dir, "other.json"), OwnerMarker{InstanceID: "other", TmuxName: "manual", CreatedAt: now.Add(-time.Hour)})

	markers, err := FindStaleOwnedSessions(StaleCleanupOptions{MarkerDir: dir, OlderThan: 30 * time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 || markers[0].TmuxName != "claude-old" {
		t.Fatalf("markers = %+v", markers)
	}
}

func TestOwnerMarkerMatchesEnvRejectsForeignReusedName(t *testing.T) {
	marker := OwnerMarker{InstanceID: "owned", TmuxName: "claude-reused", ClaudeSessionID: "session-owned"}
	foreign := map[string]string{
		ownerInstanceEnv:      "foreign",
		ownerTmuxNameEnv:      "claude-reused",
		ownerClaudeSessionEnv: "session-foreign",
	}
	if ownerMarkerMatchesEnv(marker, foreign) {
		t.Fatal("foreign tmux ownership matched stale marker")
	}

	owned := map[string]string{
		ownerInstanceEnv:      marker.InstanceID,
		ownerTmuxNameEnv:      marker.TmuxName,
		ownerClaudeSessionEnv: marker.ClaudeSessionID,
	}
	if !ownerMarkerMatchesEnv(marker, owned) {
		t.Fatal("matching tmux ownership was rejected")
	}
}

func writeMarker(t *testing.T, path string, marker OwnerMarker) {
	t.Helper()
	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

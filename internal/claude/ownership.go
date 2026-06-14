package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const ownerMarkerPrefix = "claude-acp-adapter-owned-"

const (
	ownerInstanceEnv      = "CLAUDE_ACP_OWNER_INSTANCE_ID"
	ownerPIDEnv           = "CLAUDE_ACP_OWNER_PID"
	ownerTmuxNameEnv      = "CLAUDE_ACP_OWNER_TMUX_NAME"
	ownerClaudeSessionEnv = "CLAUDE_ACP_OWNER_CLAUDE_SESSION_ID"
)

type OwnerMarker struct {
	InstanceID      string    `json:"instanceId"`
	PID             int       `json:"pid"`
	TmuxName        string    `json:"tmuxName"`
	ClaudeSessionID string    `json:"claudeSessionId"`
	WorkingDir      string    `json:"cwd"`
	FIFOPath        string    `json:"fifoPath"`
	CreatedAt       time.Time `json:"createdAt"`
}

type StaleCleanupOptions struct {
	MarkerDir string
	OlderThan time.Duration
	Now       func() time.Time
}

func newOwnerMarker(tmuxName, claudeSessionID, cwd, fifoPath string) (OwnerMarker, string, error) {
	marker := OwnerMarker{
		InstanceID:      uuid.NewString(),
		PID:             os.Getpid(),
		TmuxName:        tmuxName,
		ClaudeSessionID: claudeSessionID,
		WorkingDir:      cwd,
		FIFOPath:        fifoPath,
		CreatedAt:       time.Now(),
	}
	if err := os.MkdirAll(defaultMarkerDir(), 0o700); err != nil {
		return OwnerMarker{}, "", err
	}
	path := filepath.Join(defaultMarkerDir(), ownerMarkerPrefix+marker.InstanceID+".json")
	data, err := json.Marshal(marker)
	if err != nil {
		return OwnerMarker{}, "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return OwnerMarker{}, "", err
	}
	return marker, path, nil
}

func (m OwnerMarker) Env() map[string]string {
	return map[string]string{
		ownerInstanceEnv:      m.InstanceID,
		ownerPIDEnv:           fmt.Sprint(m.PID),
		ownerTmuxNameEnv:      m.TmuxName,
		ownerClaudeSessionEnv: m.ClaudeSessionID,
	}
}

func removeOwnerMarker(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

func FindStaleOwnedSessions(options StaleCleanupOptions) ([]OwnerMarker, error) {
	options = normalizeStaleCleanupOptions(options)
	entries, err := os.ReadDir(options.MarkerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []OwnerMarker
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ownerMarkerPrefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(options.MarkerDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var marker OwnerMarker
		if err := json.Unmarshal(data, &marker); err != nil || !validOwnerMarker(marker) {
			continue
		}
		if options.Now().Sub(marker.CreatedAt) >= options.OlderThan {
			out = append(out, marker)
		}
	}
	return out, nil
}

func CleanupStaleOwnedSessions(ctx context.Context, options StaleCleanupOptions) []error {
	options = normalizeStaleCleanupOptions(options)
	markers, err := FindStaleOwnedSessions(options)
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, marker := range markers {
		if !liveTmuxMatchesOwner(ctx, marker) {
			continue
		}
		if err := (&TmuxSession{Name: marker.TmuxName}).Kill(ctx); err != nil {
			errs = append(errs, err)
			if (&TmuxSession{Name: marker.TmuxName}).IsAlive(ctx) && liveTmuxMatchesOwner(ctx, marker) {
				continue
			}
		}
		if marker.FIFOPath != "" {
			_ = os.Remove(marker.FIFOPath)
		}
		removeOwnerMarker(filepath.Join(options.MarkerDir, ownerMarkerPrefix+marker.InstanceID+".json"))
	}
	return errs
}

func normalizeStaleCleanupOptions(options StaleCleanupOptions) StaleCleanupOptions {
	if options.MarkerDir == "" {
		options.MarkerDir = defaultMarkerDir()
	}
	if options.OlderThan == 0 {
		options.OlderThan = 30 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func defaultMarkerDir() string {
	return filepath.Join(os.TempDir(), "claude-acp-adapter-owned")
}

func validOwnerMarker(marker OwnerMarker) bool {
	return marker.InstanceID != "" && marker.TmuxName != "" && marker.CreatedAt.IsZero() == false
}

func liveTmuxMatchesOwner(ctx context.Context, marker OwnerMarker) bool {
	env, err := tmuxOwnershipEnv(ctx, marker.TmuxName)
	if err != nil {
		return false
	}
	return ownerMarkerMatchesEnv(marker, env)
}

func ownerMarkerMatchesEnv(marker OwnerMarker, env map[string]string) bool {
	return marker.InstanceID != "" &&
		env[ownerInstanceEnv] == marker.InstanceID &&
		env[ownerTmuxNameEnv] == marker.TmuxName &&
		env[ownerClaudeSessionEnv] == marker.ClaudeSessionID
}

func tmuxOwnershipEnv(ctx context.Context, tmuxName string) (map[string]string, error) {
	env := make(map[string]string)
	for _, key := range []string{ownerInstanceEnv, ownerTmuxNameEnv, ownerClaudeSessionEnv} {
		line, err := output(ctx, "tmux", "show-environment", "-t", tmuxName, key)
		if err != nil {
			return nil, err
		}
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || name != key {
			return nil, fmt.Errorf("tmux ownership env %s missing for %s", key, tmuxName)
		}
		env[name] = value
	}
	return env, nil
}

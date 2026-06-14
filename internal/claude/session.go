package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func projectsRoot(configDir string) string {
	if configDir != "" {
		return filepath.Join(configDir, "projects")
	}
	return filepath.Join(os.Getenv("HOME"), ".claude", "projects")
}

func transcriptRoots(configDir string) []string {
	primary := projectsRoot(configDir)
	roots := []string{primary}
	for _, candidate := range homeClaudeProjectRoots() {
		if candidate != primary {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func homeClaudeProjectRoots() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}

	var roots []string
	defaultRoot := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(defaultRoot); err == nil {
		roots = append(roots, defaultRoot)
	}

	matches, err := filepath.Glob(filepath.Join(home, ".claude-*", "projects"))
	if err != nil {
		return roots
	}
	return append(roots, matches...)
}

func findTranscript(ctx context.Context, roots []string, sessionID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, root := range roots {
			found := findTranscriptOnce(root, sessionID)
			if found != "" {
				return found, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("transcript for session %s not found under %v", sessionID, roots)
}

func findTranscriptOnce(root string, sessionID string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found != "" {
			return nil
		}
		if entry.Name() == sessionID+".jsonl" {
			found = path
		}
		return nil
	})
	return found
}

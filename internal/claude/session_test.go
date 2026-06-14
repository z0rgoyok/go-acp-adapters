package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindTranscriptSearchesAllRoots(t *testing.T) {
	first := filepath.Join(t.TempDir(), "projects")
	second := filepath.Join(t.TempDir(), "projects")
	sessionID := "session-123"
	path := filepath.Join(second, "project", sessionID+".jsonl")

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := findTranscript(context.Background(), []string{first, second}, sessionID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if found != path {
		t.Fatalf("found %q, want %q", found, path)
	}
}

func TestTranscriptRootsIncludesClaudeProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultRoot := filepath.Join(home, ".claude", "projects")
	altRoot := filepath.Join(home, ".claude-alt", "projects")
	unrelatedRoot := filepath.Join(home, ".claudexyz", "projects")

	if err := os.MkdirAll(defaultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(altRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unrelatedRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	roots := transcriptRoots("")
	if len(roots) != 2 {
		t.Fatalf("roots = %v", roots)
	}
	if roots[0] != defaultRoot {
		t.Fatalf("primary root = %q, want %q", roots[0], defaultRoot)
	}
	if roots[1] != altRoot {
		t.Fatalf("fallback root = %q, want %q", roots[1], altRoot)
	}
	for _, root := range roots {
		if root == unrelatedRoot {
			t.Fatalf("unexpected unrelated root %q in %v", unrelatedRoot, roots)
		}
	}
}

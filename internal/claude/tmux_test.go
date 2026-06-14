package claude

import (
	"slices"
	"testing"
)

func TestTmuxSessionLaunchArgsAddsEnvironment(t *testing.T) {
	session := &TmuxSession{
		Name:       "claude-test",
		WorkingDir: "/work",
		ClaudePath: "claude",
		Env: map[string]string{
			"Z_VAR": "last",
			"A_VAR": "first",
		},
	}

	args := session.launchArgs([]string{"--session-id", "s1"})
	want := []string{
		"new-session", "-d", "-s", "claude-test", "-c", "/work", "-x", "300", "-y", "100",
		"-e", "A_VAR=first", "-e", "Z_VAR=last", "--", "claude", "--session-id", "s1",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %#v", args)
	}
}

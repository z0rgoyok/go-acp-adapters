package claude

import "testing"

func TestRuntimeEnvCopiesAllowedValues(t *testing.T) {
	env := runtimeEnvFrom("/config", []string{
		"HOME=/home/me",
		"PATH=/bin",
		"COGERENTOR_RUN=1",
		"ANTHROPIC_API_KEY=secret",
		"UNRELATED=value",
		"CLAUDE_CONFIG_DIR=/old",
	})

	if env["HOME"] != "/home/me" || env["PATH"] != "/bin" || env["COGERENTOR_RUN"] != "1" || env["ANTHROPIC_API_KEY"] != "secret" {
		t.Fatalf("env = %#v", env)
	}
	if env["CLAUDE_CONFIG_DIR"] != "/config" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", env["CLAUDE_CONFIG_DIR"])
	}
	if _, ok := env["UNRELATED"]; ok {
		t.Fatalf("unexpected unrelated env: %#v", env)
	}
}

func TestRuntimeEnvCopiesClaudeConfigDirFromEnvironment(t *testing.T) {
	env := runtimeEnvFrom("", []string{"CLAUDE_CONFIG_DIR=/current"})

	if env["CLAUDE_CONFIG_DIR"] != "/current" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", env["CLAUDE_CONFIG_DIR"])
	}
}

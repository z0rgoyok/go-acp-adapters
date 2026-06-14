package claude

import "testing"

func TestTranscriptDiagnosticsDisabledByDefault(t *testing.T) {
	t.Setenv("CLAUDE_ACP_DEBUG_TRANSCRIPT", "")

	if transcriptDiagnosticsEnabled() {
		t.Fatal("transcript diagnostics enabled by default")
	}
}

func TestTranscriptDiagnosticsEnabledByEnv(t *testing.T) {
	t.Setenv("CLAUDE_ACP_DEBUG_TRANSCRIPT", "1")

	if !transcriptDiagnosticsEnabled() {
		t.Fatal("transcript diagnostics disabled with CLAUDE_ACP_DEBUG_TRANSCRIPT=1")
	}
}

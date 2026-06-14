package claude

import (
	"os"
	"strings"
)

func runtimeEnv(configDir string) map[string]string {
	return runtimeEnvFrom(configDir, os.Environ())
}

func runtimeEnvFrom(configDir string, environ []string) map[string]string {
	out := map[string]string{}
	for _, item := range environ {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		if runtimeEnvAllowed(key) {
			out[key] = value
		}
	}
	if configDir != "" {
		out["CLAUDE_CONFIG_DIR"] = configDir
	}
	return out
}

func runtimeEnvAllowed(key string) bool {
	switch key {
	case "HOME", "PATH", "SHELL", "TMPDIR", "USER", "LOGNAME", "CLAUDE_CONFIG_DIR":
		return true
	}
	for _, prefix := range []string{"COGERENTOR_", "CLAUDE_ACP_", "ACP_", "MCP_", "ANTHROPIC_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

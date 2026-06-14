package app

import (
	"os"
	"strings"
	"testing"

	"claude-acp-adapter/internal/acp"
)

func TestBuildMCPConfigWritesStdioServers(t *testing.T) {
	path, err := buildMCPConfig([]acp.McpServer{{Name: "fs", Type: "stdio", Command: "node", Args: []string{"server.js"}, Env: []acp.EnvVariable{{Name: "ROOT", Value: "/tmp"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"fs"`) || !strings.Contains(text, `"command": "node"`) || !strings.Contains(text, `"ROOT": "/tmp"`) {
		t.Fatalf("config = %s", text)
	}
}

func TestBuildMCPConfigRejectsUnsupportedTransport(t *testing.T) {
	_, err := buildMCPConfig([]acp.McpServer{{Name: "web", Type: "http", Command: "server"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported MCP transport") {
		t.Fatalf("err = %v", err)
	}
}

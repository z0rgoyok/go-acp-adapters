package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"claude-acp-adapter/internal/acp"
)

type mcpConfig struct {
	MCPServers map[string]mcpConfigServer `json:"mcpServers"`
}

type mcpConfigServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func buildMCPConfig(servers []acp.McpServer) (string, error) {
	if len(servers) == 0 {
		return "", nil
	}
	config := mcpConfig{MCPServers: map[string]mcpConfigServer{}}
	for i, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			name = fmt.Sprintf("server-%d", i+1)
		}
		serverType := strings.TrimSpace(server.Type)
		if serverType == "" {
			serverType = "stdio"
		}
		if serverType != "stdio" {
			return "", invalidParams("unsupported MCP transport: " + serverType)
		}
		if strings.TrimSpace(server.Command) == "" {
			return "", invalidParams("stdio MCP server command is required")
		}
		config.MCPServers[name] = mcpConfigServer{
			Command: server.Command,
			Args:    append([]string(nil), server.Args...),
			Env:     mcpEnv(server.Env),
		}
	}
	file, err := os.CreateTemp("", "claude-acp-mcp-*.json")
	if err != nil {
		return "", err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		file.Close()
		_ = os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func mcpEnv(env []acp.EnvVariable) map[string]string {
	if len(env) == 0 {
		return nil
	}
	result := make(map[string]string, len(env))
	for _, variable := range env {
		name := strings.TrimSpace(variable.Name)
		if name != "" {
			result[name] = variable.Value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

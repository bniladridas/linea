package agent

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

func WithMCPConfigPath(path string) func(*Runtime) {
	return func(r *Runtime) {
		r.mcpConfigPath = strings.TrimSpace(path)
	}
}

func (r *Runtime) ListMCPServers(ctx context.Context) []MCPServer {
	return r.mcpServers(ctx)
}

func (r *Runtime) mcpServers(ctx context.Context) []MCPServer {
	if strings.TrimSpace(r.mcpConfigPath) == "" {
		return []MCPServer{}
	}
	select {
	case <-ctx.Done():
		return []MCPServer{{ID: "mcp", Name: "MCP", State: "unavailable"}}
	default:
	}
	data, err := os.ReadFile(r.mcpConfigPath)
	if err != nil {
		return []MCPServer{{ID: "mcp", Name: "MCP", State: "unavailable"}}
	}
	var config struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return []MCPServer{{ID: "mcp", Name: "MCP", State: "unavailable"}}
	}
	names := make([]string, 0, len(config.MCPServers))
	for name := range config.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]MCPServer, 0, len(names))
	for _, name := range names {
		item := config.MCPServers[name]
		state := "ready"
		command := strings.Join(strings.Fields(item.Command), " ")
		if command == "" {
			state = "unavailable"
		}
		envKeys := make([]string, 0, len(item.Env))
		for key := range item.Env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)
		servers = append(servers, MCPServer{
			ID:      skillIDFromFile(name),
			Name:    name,
			State:   state,
			Command: command,
			Args:    append([]string(nil), item.Args...),
			EnvKeys: envKeys,
		})
	}
	return servers
}

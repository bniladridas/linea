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

func (r *Runtime) ListMCPTools(ctx context.Context) []MCPTool {
	return r.mcpTools(ctx)
}

func (r *Runtime) mcpServers(ctx context.Context) []MCPServer {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		if strings.TrimSpace(r.mcpConfigPath) == "" {
			return []MCPServer{}
		}
		return []MCPServer{{ID: "mcp", Name: "MCP", State: "unavailable"}}
	}
	return mcpServersFromConfig(config)
}

func (r *Runtime) mcpTools(ctx context.Context) []MCPTool {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPTool{}
	}
	servers := mcpServersFromConfig(config)
	serverByName := map[string]MCPServer{}
	for _, server := range servers {
		serverByName[server.Name] = server
	}
	names := make([]string, 0, len(config.MCPServers))
	for name := range config.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := []MCPTool{}
	for _, serverName := range names {
		server := serverByName[serverName]
		item := config.MCPServers[serverName]
		for _, tool := range item.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			tools = append(tools, MCPTool{
				ID:          server.ID + "/" + skillIDFromFile(name),
				ServerID:    server.ID,
				ServerName:  server.Name,
				Name:        name,
				Description: strings.TrimSpace(tool.Description),
				State:       server.State,
			})
		}
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].ServerName == tools[j].ServerName {
			return tools[i].Name < tools[j].Name
		}
		return tools[i].ServerName < tools[j].ServerName
	})
	return tools
}

type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Tools   []mcpToolConfig   `json:"tools"`
}

type mcpToolConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (r *Runtime) loadMCPConfig(ctx context.Context) (mcpConfig, bool) {
	if strings.TrimSpace(r.mcpConfigPath) == "" {
		return mcpConfig{}, false
	}
	select {
	case <-ctx.Done():
		return mcpConfig{}, false
	default:
	}
	data, err := os.ReadFile(r.mcpConfigPath)
	if err != nil {
		return mcpConfig{}, false
	}
	var config mcpConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return mcpConfig{}, false
	}
	return config, true
}

func mcpServersFromConfig(config mcpConfig) []MCPServer {
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

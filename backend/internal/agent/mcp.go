package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	mcpCallTimeout   = 15 * time.Second
	mcpShutdownGrace = 500 * time.Millisecond
	maxMCPOutput     = 64 * 1024
	maxMCPCalls      = 50
	maxMCPToolPages  = 20
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

func (r *Runtime) CallMCPTool(ctx context.Context, input MCPCallInput) (MCPCall, error) {
	toolID := strings.TrimSpace(input.ToolID)
	if toolID == "" {
		return MCPCall{}, errors.New("MCP tool ID is required.")
	}
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return MCPCall{}, errors.New("MCP config is not available.")
	}
	serverName, server, tool, ok := r.findMCPToolConfig(ctx, config, toolID)
	if !ok {
		return MCPCall{}, errors.New("MCP tool was not found.")
	}
	if strings.TrimSpace(server.Command) == "" {
		return MCPCall{}, errors.New("MCP server command is required.")
	}
	call := MCPCall{
		ID:        newTraceID(),
		ToolID:    toolID,
		ServerID:  skillIDFromFile(serverName),
		Name:      strings.TrimSpace(tool.Name),
		State:     "completed",
		CreatedAt: time.Now().UTC(),
	}
	output, err := callMCPTool(ctx, server, tool, input.Arguments)
	if err != nil {
		call.State = "failed"
		call.Error = err.Error()
	} else {
		call.Output, call.Truncated = truncateMCPOutput(output)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpCalls = append([]MCPCall{call}, r.mcpCalls...)
	if len(r.mcpCalls) > maxMCPCalls {
		r.mcpCalls = r.mcpCalls[:maxMCPCalls]
	}
	if err != nil {
		return call, err
	}
	return call, nil
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
	return r.mcpToolsFromConfig(ctx, config, true)
}

func (r *Runtime) statusMCPTools(ctx context.Context) []MCPTool {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPTool{}
	}
	return r.mcpToolsFromConfig(ctx, config, false)
}

func (r *Runtime) mcpToolsFromConfig(ctx context.Context, config mcpConfig, discover bool) []MCPTool {
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
		for _, tool := range r.mcpToolsForServer(ctx, item, discover) {
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

func (r *Runtime) mcpToolsForServer(ctx context.Context, server mcpServerConfig, discover bool) []mcpToolConfig {
	if len(server.Tools) > 0 {
		return append([]mcpToolConfig(nil), server.Tools...)
	}
	if !discover || strings.TrimSpace(server.Command) == "" {
		return []mcpToolConfig{}
	}
	tools, err := listMCPTools(ctx, server)
	if err != nil {
		return []mcpToolConfig{}
	}
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

func (r *Runtime) findMCPToolConfig(ctx context.Context, config mcpConfig, toolID string) (string, mcpServerConfig, mcpToolConfig, bool) {
	names := make([]string, 0, len(config.MCPServers))
	for serverName := range config.MCPServers {
		names = append(names, serverName)
	}
	sort.Strings(names)
	for _, serverName := range names {
		server := config.MCPServers[serverName]
		serverID := skillIDFromFile(serverName)
		for _, tool := range r.mcpToolsForServer(ctx, server, true) {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			id := serverID + "/" + skillIDFromFile(name)
			if id == toolID || name == toolID {
				return serverName, server, tool, true
			}
		}
	}
	return "", mcpServerConfig{}, mcpToolConfig{}, false
}

func callMCPTool(ctx context.Context, server mcpServerConfig, tool mcpToolConfig, arguments map[string]any) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	cmd, stdin, reader, err := startMCPCommand(callCtx, server)
	if err != nil {
		return "", err
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	if err := writeMCPMessage(stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      strings.TrimSpace(tool.Name),
			"arguments": arguments,
		},
	}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}
	response, err := readMCPResponse(reader, 2)
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if callCtx.Err() == context.DeadlineExceeded {
			return "", errors.New("MCP call timed out.")
		}
		return "", err
	}
	shutdownMCPCommand(cmd, stdin)
	if errValue, ok := response["error"]; ok {
		data, _ := json.Marshal(errValue)
		return "", fmt.Errorf("MCP call failed: %s", string(data))
	}
	result, ok := response["result"]
	if !ok {
		return "", errors.New("MCP response missing result.")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func listMCPTools(ctx context.Context, server mcpServerConfig) ([]mcpToolConfig, error) {
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	cmd, stdin, reader, err := startMCPCommand(callCtx, server)
	if err != nil {
		return nil, err
	}
	tools := []mcpToolConfig{}
	cursor := ""
	for page := 0; page < maxMCPToolPages; page++ {
		id := 2 + page
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := writeMCPMessage(stdin, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/list",
			"params":  params,
		}); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, err
		}
		response, err := readMCPResponse(reader, id)
		if err != nil {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			if callCtx.Err() == context.DeadlineExceeded {
				return nil, errors.New("MCP tools/list timed out.")
			}
			return nil, err
		}
		if errValue, ok := response["error"]; ok {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			data, _ := json.Marshal(errValue)
			return nil, fmt.Errorf("MCP tools/list failed: %s", string(data))
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, errors.New("MCP tools/list response missing result.")
		}
		items, _ := result["tools"].([]any)
		for _, item := range items {
			tool, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := tool["name"].(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			description, _ := tool["description"].(string)
			tools = append(tools, mcpToolConfig{Name: name, Description: description})
		}
		nextCursor, _ := result["nextCursor"].(string)
		cursor = strings.TrimSpace(nextCursor)
		if cursor == "" {
			shutdownMCPCommand(cmd, stdin)
			return tools, nil
		}
	}
	shutdownMCPCommand(cmd, stdin)
	return tools, nil
}

func startMCPCommand(ctx context.Context, server mcpServerConfig) (*exec.Cmd, io.WriteCloser, *bufio.Reader, error) {
	args := append([]string(nil), server.Args...)
	cmd := exec.CommandContext(ctx, strings.TrimSpace(server.Command), args...)
	cmd.Env = os.Environ()
	for key, value := range server.Env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	reader := bufio.NewReader(stdout)
	if err := initializeMCPCommand(cmd, stdin, reader); err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdin, reader, nil
}

func initializeMCPCommand(cmd *exec.Cmd, stdin io.Writer, reader *bufio.Reader) error {
	if err := writeMCPMessage(stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "linea",
				"version": "0",
			},
		},
	}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if _, err := readMCPResponse(reader, 1); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := writeMCPMessage(stdin, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return nil
}

func shutdownMCPCommand(cmd *exec.Cmd, stdin io.Closer) {
	_ = stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(mcpShutdownGrace):
		_ = cmd.Process.Kill()
		<-done
	}
}

func writeMCPMessage(writer io.Writer, message map[string]any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func readMCPResponse(reader *bufio.Reader, id int) (map[string]any, error) {
	for {
		message, err := readMCPMessage(reader)
		if err != nil {
			return nil, err
		}
		value, ok := message["id"]
		if !ok || intID(value) != id {
			continue
		}
		return message, nil
	}
}

func readMCPMessage(reader *bufio.Reader) (map[string]any, error) {
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if key, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			length = parsed
		}
	}
	if length <= 0 {
		return nil, errors.New("MCP response missing content length.")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	return message, nil
}

func intID(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func truncateMCPOutput(output string) (string, bool) {
	if len(output) <= maxMCPOutput {
		return output, false
	}
	return output[:maxMCPOutput], true
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

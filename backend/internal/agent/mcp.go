package agent

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	maxMCPListPages  = 20
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

func (r *Runtime) ListMCPResources(ctx context.Context) []MCPResource {
	return r.mcpResources(ctx)
}

func (r *Runtime) ListMCPPrompts(ctx context.Context) []MCPPrompt {
	return r.mcpPrompts(ctx)
}

func (r *Runtime) ReadMCPResource(ctx context.Context, input MCPResourceReadInput) (MCPCall, error) {
	resourceID := strings.TrimSpace(input.ResourceID)
	uri := strings.TrimSpace(input.URI)
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return MCPCall{}, errors.New("MCP config is not available.")
	}
	serverName, server, resource, ok := r.findMCPResourceConfig(ctx, config, resourceID, uri)
	if !ok {
		return MCPCall{}, errors.New("MCP resource was not found.")
	}
	if strings.TrimSpace(server.Command) == "" {
		return MCPCall{}, errors.New("MCP server command is required.")
	}
	if uri == "" {
		uri = strings.TrimSpace(resource.URI)
	}
	call := MCPCall{
		ID:        newTraceID(),
		ToolID:    "resource:" + uri,
		ServerID:  skillIDFromFile(serverName),
		Name:      strings.TrimSpace(resource.Name),
		State:     "completed",
		CreatedAt: time.Now().UTC(),
	}
	output, err := readMCPResource(ctx, server, uri)
	return r.recordMCPCall(call, output, err)
}

func (r *Runtime) GetMCPPrompt(ctx context.Context, input MCPPromptGetInput) (MCPCall, error) {
	promptID := strings.TrimSpace(input.PromptID)
	name := strings.TrimSpace(input.Name)
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return MCPCall{}, errors.New("MCP config is not available.")
	}
	serverName, server, prompt, ok := r.findMCPPromptConfig(ctx, config, promptID, name)
	if !ok {
		return MCPCall{}, errors.New("MCP prompt was not found.")
	}
	if strings.TrimSpace(server.Command) == "" {
		return MCPCall{}, errors.New("MCP server command is required.")
	}
	if name == "" {
		name = strings.TrimSpace(prompt.Name)
	}
	call := MCPCall{
		ID:        newTraceID(),
		ToolID:    "prompt:" + name,
		ServerID:  skillIDFromFile(serverName),
		Name:      name,
		State:     "completed",
		CreatedAt: time.Now().UTC(),
	}
	output, err := getMCPPrompt(ctx, server, name, input.Arguments)
	return r.recordMCPCall(call, output, err)
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
	return r.recordMCPCall(call, output, err)
}

func (r *Runtime) recordMCPCall(call MCPCall, output string, err error) (MCPCall, error) {
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

func (r *Runtime) mcpResources(ctx context.Context) []MCPResource {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPResource{}
	}
	return r.mcpResourcesFromConfig(ctx, config, true)
}

func (r *Runtime) statusMCPResources(ctx context.Context) []MCPResource {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPResource{}
	}
	return r.mcpResourcesFromConfig(ctx, config, false)
}

func (r *Runtime) mcpPrompts(ctx context.Context) []MCPPrompt {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPPrompt{}
	}
	return r.mcpPromptsFromConfig(ctx, config, true)
}

func (r *Runtime) statusMCPPrompts(ctx context.Context) []MCPPrompt {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPPrompt{}
	}
	return r.mcpPromptsFromConfig(ctx, config, false)
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
				InputSchema: mcpInputSchemaString(tool.InputSchema),
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

func (r *Runtime) mcpResourcesFromConfig(ctx context.Context, config mcpConfig, discover bool) []MCPResource {
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
	resources := []MCPResource{}
	for _, serverName := range names {
		server := serverByName[serverName]
		item := config.MCPServers[serverName]
		for _, resource := range r.mcpResourcesForServer(ctx, item, discover) {
			uri := strings.TrimSpace(resource.URI)
			name := strings.TrimSpace(resource.Name)
			if uri == "" {
				continue
			}
			if name == "" {
				name = uri
			}
			resources = append(resources, MCPResource{
				ID:          server.ID + "/" + mcpURIID(uri),
				ServerID:    server.ID,
				ServerName:  server.Name,
				URI:         uri,
				Name:        name,
				Description: strings.TrimSpace(resource.Description),
				MimeType:    strings.TrimSpace(resource.MimeType),
				State:       server.State,
			})
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].ServerName == resources[j].ServerName {
			return resources[i].URI < resources[j].URI
		}
		return resources[i].ServerName < resources[j].ServerName
	})
	return resources
}

func (r *Runtime) mcpPromptsFromConfig(ctx context.Context, config mcpConfig, discover bool) []MCPPrompt {
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
	prompts := []MCPPrompt{}
	for _, serverName := range names {
		server := serverByName[serverName]
		item := config.MCPServers[serverName]
		for _, prompt := range r.mcpPromptsForServer(ctx, item, discover) {
			name := strings.TrimSpace(prompt.Name)
			if name == "" {
				continue
			}
			prompts = append(prompts, MCPPrompt{
				ID:          server.ID + "/" + mcpPromptID(name),
				ServerID:    server.ID,
				ServerName:  server.Name,
				Name:        name,
				Description: strings.TrimSpace(prompt.Description),
				State:       server.State,
			})
		}
	}
	sort.Slice(prompts, func(i, j int) bool {
		if prompts[i].ServerName == prompts[j].ServerName {
			return prompts[i].Name < prompts[j].Name
		}
		return prompts[i].ServerName < prompts[j].ServerName
	})
	return prompts
}

func (r *Runtime) mcpResourcesForServer(ctx context.Context, server mcpServerConfig, discover bool) []mcpResourceConfig {
	if len(server.Resources) > 0 {
		return append([]mcpResourceConfig(nil), server.Resources...)
	}
	if !discover || strings.TrimSpace(server.Command) == "" {
		return []mcpResourceConfig{}
	}
	resources, err := listMCPResources(ctx, server)
	if err != nil {
		return []mcpResourceConfig{}
	}
	return resources
}

func (r *Runtime) mcpPromptsForServer(ctx context.Context, server mcpServerConfig, discover bool) []mcpPromptConfig {
	if len(server.Prompts) > 0 {
		return append([]mcpPromptConfig(nil), server.Prompts...)
	}
	if !discover || strings.TrimSpace(server.Command) == "" {
		return []mcpPromptConfig{}
	}
	prompts, err := listMCPPrompts(ctx, server)
	if err != nil {
		return []mcpPromptConfig{}
	}
	return prompts
}

type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command   string              `json:"command"`
	Args      []string            `json:"args"`
	Env       map[string]string   `json:"env"`
	Tools     []mcpToolConfig     `json:"tools"`
	Resources []mcpResourceConfig `json:"resources"`
	Prompts   []mcpPromptConfig   `json:"prompts"`
}

type mcpToolConfig struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpResourceConfig struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

type mcpPromptConfig struct {
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

func (r *Runtime) findMCPResourceConfig(ctx context.Context, config mcpConfig, resourceID string, uri string) (string, mcpServerConfig, mcpResourceConfig, bool) {
	names := make([]string, 0, len(config.MCPServers))
	for serverName := range config.MCPServers {
		names = append(names, serverName)
	}
	sort.Strings(names)
	for _, serverName := range names {
		server := config.MCPServers[serverName]
		serverID := skillIDFromFile(serverName)
		for _, resource := range r.mcpResourcesForServer(ctx, server, true) {
			resourceURI := strings.TrimSpace(resource.URI)
			if resourceURI == "" {
				continue
			}
			id := serverID + "/" + mcpURIID(resourceURI)
			if id == resourceID || resourceURI == uri {
				return serverName, server, resource, true
			}
		}
	}
	return "", mcpServerConfig{}, mcpResourceConfig{}, false
}

func (r *Runtime) findMCPPromptConfig(ctx context.Context, config mcpConfig, promptID string, name string) (string, mcpServerConfig, mcpPromptConfig, bool) {
	names := make([]string, 0, len(config.MCPServers))
	for serverName := range config.MCPServers {
		names = append(names, serverName)
	}
	sort.Strings(names)
	for _, serverName := range names {
		server := config.MCPServers[serverName]
		serverID := skillIDFromFile(serverName)
		for _, prompt := range r.mcpPromptsForServer(ctx, server, true) {
			promptName := strings.TrimSpace(prompt.Name)
			if promptName == "" {
				continue
			}
			id := serverID + "/" + mcpPromptID(promptName)
			if id == promptID || promptName == name {
				return serverName, server, prompt, true
			}
		}
	}
	return "", mcpServerConfig{}, mcpPromptConfig{}, false
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
			var inputSchema json.RawMessage
			if schema, ok := tool["inputSchema"]; ok {
				data, err := json.Marshal(schema)
				if err == nil {
					inputSchema = data
				}
			}
			tools = append(tools, mcpToolConfig{Name: name, Description: description, InputSchema: inputSchema})
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

func listMCPResources(ctx context.Context, server mcpServerConfig) ([]mcpResourceConfig, error) {
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	cmd, stdin, reader, err := startMCPCommand(callCtx, server)
	if err != nil {
		return nil, err
	}
	resources := []mcpResourceConfig{}
	cursor := ""
	for page := 0; page < maxMCPListPages; page++ {
		id := 2 + page
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := writeMCPMessage(stdin, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "resources/list",
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
				return nil, errors.New("MCP resources/list timed out.")
			}
			return nil, err
		}
		if errValue, ok := response["error"]; ok {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			data, _ := json.Marshal(errValue)
			return nil, fmt.Errorf("MCP resources/list failed: %s", string(data))
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, errors.New("MCP resources/list response missing result.")
		}
		items, _ := result["resources"].([]any)
		for _, item := range items {
			resource, ok := item.(map[string]any)
			if !ok {
				continue
			}
			uri, _ := resource["uri"].(string)
			if strings.TrimSpace(uri) == "" {
				continue
			}
			name, _ := resource["name"].(string)
			description, _ := resource["description"].(string)
			mimeType, _ := resource["mimeType"].(string)
			resources = append(resources, mcpResourceConfig{URI: uri, Name: name, Description: description, MimeType: mimeType})
		}
		nextCursor, _ := result["nextCursor"].(string)
		cursor = strings.TrimSpace(nextCursor)
		if cursor == "" {
			shutdownMCPCommand(cmd, stdin)
			return resources, nil
		}
	}
	shutdownMCPCommand(cmd, stdin)
	return resources, nil
}

func listMCPPrompts(ctx context.Context, server mcpServerConfig) ([]mcpPromptConfig, error) {
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	cmd, stdin, reader, err := startMCPCommand(callCtx, server)
	if err != nil {
		return nil, err
	}
	prompts := []mcpPromptConfig{}
	cursor := ""
	for page := 0; page < maxMCPListPages; page++ {
		id := 2 + page
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := writeMCPMessage(stdin, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "prompts/list",
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
				return nil, errors.New("MCP prompts/list timed out.")
			}
			return nil, err
		}
		if errValue, ok := response["error"]; ok {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			data, _ := json.Marshal(errValue)
			return nil, fmt.Errorf("MCP prompts/list failed: %s", string(data))
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, errors.New("MCP prompts/list response missing result.")
		}
		items, _ := result["prompts"].([]any)
		for _, item := range items {
			prompt, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := prompt["name"].(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			description, _ := prompt["description"].(string)
			prompts = append(prompts, mcpPromptConfig{Name: name, Description: description})
		}
		nextCursor, _ := result["nextCursor"].(string)
		cursor = strings.TrimSpace(nextCursor)
		if cursor == "" {
			shutdownMCPCommand(cmd, stdin)
			return prompts, nil
		}
	}
	shutdownMCPCommand(cmd, stdin)
	return prompts, nil
}

func readMCPResource(ctx context.Context, server mcpServerConfig, uri string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	cmd, stdin, reader, err := startMCPCommand(callCtx, server)
	if err != nil {
		return "", err
	}
	if err := writeMCPMessage(stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/read",
		"params":  map[string]any{"uri": uri},
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
			return "", errors.New("MCP resources/read timed out.")
		}
		return "", err
	}
	shutdownMCPCommand(cmd, stdin)
	if errValue, ok := response["error"]; ok {
		data, _ := json.Marshal(errValue)
		return "", fmt.Errorf("MCP resources/read failed: %s", string(data))
	}
	result, ok := response["result"]
	if !ok {
		return "", errors.New("MCP resources/read response missing result.")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func getMCPPrompt(ctx context.Context, server mcpServerConfig, name string, arguments map[string]any) (string, error) {
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
		"method":  "prompts/get",
		"params":  map[string]any{"name": name, "arguments": arguments},
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
			return "", errors.New("MCP prompts/get timed out.")
		}
		return "", err
	}
	shutdownMCPCommand(cmd, stdin)
	if errValue, ok := response["error"]; ok {
		data, _ := json.Marshal(errValue)
		return "", fmt.Errorf("MCP prompts/get failed: %s", string(data))
	}
	result, ok := response["result"]
	if !ok {
		return "", errors.New("MCP prompts/get response missing result.")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
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

func mcpInputSchemaString(schema json.RawMessage) string {
	data := []byte(strings.TrimSpace(string(schema)))
	if len(data) == 0 {
		return ""
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err == nil {
			return strings.TrimSpace(value)
		}
	}
	if json.Valid(data) {
		return string(data)
	}
	return ""
}

func mcpURIID(uri string) string {
	value := strings.ToLower(strings.TrimSpace(uri))
	var b strings.Builder
	previousSeparator := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			b.WriteRune(char)
			previousSeparator = false
			continue
		}
		if !previousSeparator {
			b.WriteByte('_')
			previousSeparator = true
		}
	}
	id := strings.Trim(b.String(), "_")
	if id == "" {
		id = "resource"
	}
	sum := sha1.Sum([]byte(strings.TrimSpace(uri)))
	return id + "_" + hex.EncodeToString(sum[:4])
}

func mcpPromptID(name string) string {
	value := strings.TrimSpace(name)
	id := skillIDFromFile(value)
	if id == "" {
		id = "prompt"
	}
	sum := sha1.Sum([]byte(value))
	return id + "_" + hex.EncodeToString(sum[:4])
}

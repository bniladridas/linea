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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	mcpCallTimeout   = 15 * time.Second
	mcpShutdownGrace = 500 * time.Millisecond
	maxMCPOutput     = 64 * 1024
	maxMCPCalls      = 50
	maxMCPEvents     = 50
	maxMCPToolPages  = 20
	maxMCPListPages  = 20
)

type mcpSession struct {
	serverID   string
	serverName string
	server     mcpServerConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	reader     *bufio.Reader
	mu         sync.Mutex
	nextID     int
	pending    map[int]chan map[string]any
	closed     bool
}

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
	if len(server.AllowedTools) > 0 {
		allowed := false
		toolName := strings.TrimSpace(tool.Name)
		for _, name := range server.AllowedTools {
			if strings.TrimSpace(name) == toolName {
				allowed = true
				break
			}
		}
		if !allowed {
			return MCPCall{}, fmt.Errorf("MCP tool %q is not allowed by server configuration", tool.Name)
		}
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

func (r *Runtime) SubscribeMCPResource(ctx context.Context, input MCPSubscribeInput) (MCPSubscription, error) {
	resourceID := strings.TrimSpace(input.ResourceID)
	uri := strings.TrimSpace(input.URI)
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return MCPSubscription{}, errors.New("MCP config is not available.")
	}
	serverName, server, resource, ok := r.findMCPResourceConfig(ctx, config, resourceID, uri)
	if !ok {
		return MCPSubscription{}, errors.New("MCP resource was not found.")
	}
	if strings.TrimSpace(server.Command) == "" {
		return MCPSubscription{}, errors.New("MCP server command is required.")
	}
	if uri == "" {
		uri = strings.TrimSpace(resource.URI)
	}
	serverID := skillIDFromFile(serverName)
	now := time.Now().UTC()
	subscription := MCPSubscription{
		ID:         newTraceID(),
		ServerID:   serverID,
		ServerName: serverName,
		ResourceID: resourceID,
		URI:        uri,
		State:      "subscribing",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.mu.Lock()
	r.mcpSubscriptions = append([]MCPSubscription{subscription}, r.mcpSubscriptions...)
	r.mu.Unlock()
	session, err := r.mcpPersistentSession(serverName, server)
	if err != nil {
		r.updateMCPSubscriptionState(subscription.ID, "failed", err.Error())
		return MCPSubscription{}, err
	}
	if err := session.request(ctx, "resources/subscribe", map[string]any{"uri": uri}); err != nil {
		r.updateMCPSubscriptionState(subscription.ID, "failed", err.Error())
		return MCPSubscription{}, err
	}
	subscription.State = "active"
	subscription.UpdatedAt = time.Now().UTC()
	r.updateMCPSubscriptionState(subscription.ID, "active", "")
	return subscription, nil
}

func (r *Runtime) UnsubscribeMCPResource(ctx context.Context, id string) (MCPSubscription, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MCPSubscription{}, errors.New("MCP subscription ID is required.")
	}
	r.mu.RLock()
	var subscription MCPSubscription
	found := false
	for _, item := range r.mcpSubscriptions {
		if item.ID == id {
			subscription = item
			found = true
			break
		}
	}
	session := r.mcpSessions[subscription.ServerID]
	r.mu.RUnlock()
	if !found {
		return MCPSubscription{}, errors.New("MCP subscription was not found.")
	}
	var err error
	if session != nil {
		err = session.request(ctx, "resources/unsubscribe", map[string]any{"uri": subscription.URI})
	}
	now := time.Now().UTC()
	subscription.State = "inactive"
	subscription.UpdatedAt = now
	if err != nil {
		subscription.State = "failed"
		subscription.Error = err.Error()
	}
	r.mu.Lock()
	for index := range r.mcpSubscriptions {
		if r.mcpSubscriptions[index].ID == id {
			r.mcpSubscriptions[index] = subscription
			break
		}
	}
	r.mu.Unlock()
	r.stopIdleMCPSession(subscription.ServerID)
	if err != nil {
		return subscription, err
	}
	return subscription, nil
}

func (r *Runtime) recordMCPCall(call MCPCall, output string, err error) (MCPCall, error) {
	if err != nil {
		call.State = "failed"
		call.Error = err.Error()
	} else {
		call.Output, call.Truncated = truncateMCPOutput(redactSecrets(output))
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
	servers := mcpServersFromConfig(config)
	r.mu.RLock()
	defer r.mu.RUnlock()
	for index := range servers {
		if session := r.mcpSessions[servers[index].ID]; session != nil && !session.isClosed() {
			servers[index].State = "active"
		}
	}
	return servers
}

func (r *Runtime) mcpTools(ctx context.Context) []MCPTool {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPTool{}
	}
	return r.mcpToolsFromConfig(ctx, config, true)
}

func (r *Runtime) mcpState(ctx context.Context) string {
	servers := r.mcpServers(ctx)
	if len(servers) == 0 {
		return "unavailable"
	}
	hasActive := false
	hasReady := false
	for _, s := range servers {
		switch s.State {
		case "active":
			hasActive = true
		case "ready":
			hasReady = true
		}
	}
	switch {
	case hasActive:
		return "active"
	case hasReady:
		return "ready"
	default:
		return "unavailable"
	}
}

func (r *Runtime) statusMCPTools(ctx context.Context) []MCPTool {
	config, ok := r.loadMCPConfig(ctx)
	if !ok {
		return []MCPTool{}
	}
	return r.mcpToolsFromConfig(ctx, config, true)
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
	return r.mcpResourcesFromConfig(ctx, config, true)
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
	return r.mcpPromptsFromConfig(ctx, config, true)
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
	Command      string              `json:"command"`
	Args         []string            `json:"args"`
	Env          map[string]string   `json:"env"`
	Tools        []mcpToolConfig     `json:"tools"`
	Resources    []mcpResourceConfig `json:"resources"`
	Prompts      []mcpPromptConfig   `json:"prompts"`
	AllowedTools []string            `json:"allowedTools,omitempty"`
	Dir          string              `json:"-"`
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
	cmdPath := strings.TrimSpace(server.Command)
	if cmdPath == "" {
		return nil, nil, nil, errors.New("MCP command is required.")
	}
	if strings.Contains(cmdPath, "..") {
		return nil, nil, nil, errors.New("MCP command path traversal is not allowed.")
	}

	args := append([]string(nil), server.Args...)
	cmd := exec.CommandContext(ctx, cmdPath, args...)
	if strings.TrimSpace(server.Dir) != "" {
		cmd.Dir = server.Dir
	} else {
		cmd.Dir = os.TempDir()
	}

	var cleanEnv []string
	allowedEnvKeys := map[string]bool{
		"PATH":    true,
		"TMPDIR":  true,
		"USER":    true,
		"HOME":    true,
		"LANG":    true,
		"TZ":      true,
		"SHELL":   true,
		"LOGNAME": true,
	}
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) > 0 && allowedEnvKeys[strings.ToUpper(parts[0])] {
			cleanEnv = append(cleanEnv, env)
		}
	}
	cmd.Env = cleanEnv

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

func (r *Runtime) mcpPersistentSession(serverName string, server mcpServerConfig) (*mcpSession, error) {
	serverID := skillIDFromFile(serverName)
	r.mu.RLock()
	if session := r.mcpSessions[serverID]; session != nil && !session.isClosed() {
		r.mu.RUnlock()
		return session, nil
	}
	r.mu.RUnlock()
	cmd, stdin, reader, err := startMCPCommand(r.mcpSessionContext(), server)
	if err != nil {
		return nil, err
	}
	session := &mcpSession{
		serverID:   serverID,
		serverName: serverName,
		server:     server,
		cmd:        cmd,
		stdin:      stdin,
		reader:     reader,
		nextID:     10,
		pending:    map[int]chan map[string]any{},
	}
	r.mu.Lock()
	r.mcpSessions[serverID] = session
	r.mu.Unlock()
	go session.readLoop(r)
	return session, nil
}

func (s *mcpSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *mcpSession) request(ctx context.Context, method string, params map[string]any) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("MCP session is closed.")
	}
	id := s.nextID
	s.nextID++
	ch := make(chan map[string]any, 1)
	s.pending[id] = ch
	err := writeMCPMessage(s.stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	s.mu.Unlock()
	if err != nil {
		s.removePending(id)
		return err
	}
	select {
	case <-ctx.Done():
		s.removePending(id)
		return ctx.Err()
	case response, ok := <-ch:
		if !ok {
			return errors.New("MCP session closed.")
		}
		if errData, ok := response["error"]; ok {
			data, _ := json.Marshal(errData)
			return fmt.Errorf("MCP %s failed: %s", method, string(data))
		}
		return nil
	case <-time.After(mcpCallTimeout):
		s.removePending(id)
		return fmt.Errorf("MCP %s timed out.", method)
	}
}

func (s *mcpSession) removePending(id int) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *mcpSession) readLoop(r *Runtime) {
	for {
		message, err := readMCPMessage(s.reader)
		if err != nil {
			if s.isClosed() {
				s.closePending()
				return
			}
			s.closePending()
			r.recordMCPEvent(MCPEvent{
				ID:        newTraceID(),
				ServerID:  s.serverID,
				Method:    "session",
				Error:     err.Error(),
				CreatedAt: time.Now().UTC(),
			})
			return
		}
		if value, ok := message["id"]; ok {
			id := intID(value)
			s.mu.Lock()
			ch := s.pending[id]
			delete(s.pending, id)
			s.mu.Unlock()
			if ch != nil {
				ch <- message
			}
			continue
		}
		method, _ := message["method"].(string)
		params, _ := message["params"].(map[string]any)
		uri := mcpNotificationURI(params)
		r.recordMCPEvent(MCPEvent{
			ID:             newTraceID(),
			SubscriptionID: r.mcpSubscriptionIDForURI(s.serverID, uri),
			ServerID:       s.serverID,
			URI:            uri,
			Method:         strings.TrimSpace(method),
			Output:         redactSecrets(mcpNotificationOutput(params)),
			CreatedAt:      time.Now().UTC(),
		})
	}
}

func (s *mcpSession) closePending() {
	s.mu.Lock()
	s.closed = true
	for id, ch := range s.pending {
		delete(s.pending, id)
		close(ch)
	}
	s.mu.Unlock()
}

func (r *Runtime) recordMCPEvent(event MCPEvent) {
	r.mu.Lock()
	r.mcpEvents = append([]MCPEvent{event}, r.mcpEvents...)
	if len(r.mcpEvents) > maxMCPEvents {
		r.mcpEvents = r.mcpEvents[:maxMCPEvents]
	}
	var listeners []func(MCPEvent)
	for _, l := range r.mcpListeners {
		listeners = append(listeners, l)
	}
	r.mu.Unlock()

	for _, listener := range listeners {
		listener(event)
	}
}

func (r *Runtime) RegisterMCPListener(id string, listener func(MCPEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mcpListeners == nil {
		r.mcpListeners = map[string]func(MCPEvent){}
	}
	r.mcpListeners[id] = listener
}

func (r *Runtime) UnregisterMCPListener(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mcpListeners, id)
}

func (r *Runtime) updateMCPSubscriptionState(id string, state string, errDetail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	for index := range r.mcpSubscriptions {
		if r.mcpSubscriptions[index].ID == id {
			r.mcpSubscriptions[index].State = state
			r.mcpSubscriptions[index].Error = strings.TrimSpace(errDetail)
			r.mcpSubscriptions[index].UpdatedAt = now
			return
		}
	}
}

func (r *Runtime) mcpSubscriptionIDForURI(serverID string, uri string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.mcpSubscriptions {
		if item.ServerID == serverID && item.URI == uri && (item.State == "active" || item.State == "subscribing") {
			return item.ID
		}
	}
	return ""
}

func (r *Runtime) stopIdleMCPSession(serverID string) {
	r.mu.RLock()
	active := false
	session := r.mcpSessions[serverID]
	for _, item := range r.mcpSubscriptions {
		if item.ServerID == serverID && item.State == "active" {
			active = true
			break
		}
	}
	r.mu.RUnlock()
	if active || session == nil {
		return
	}
	r.mu.Lock()
	delete(r.mcpSessions, serverID)
	r.mu.Unlock()
	session.stop()
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.shutdownCancel != nil {
		r.shutdownCancel()
	}
	r.mu.Lock()
	sessions := make([]*mcpSession, 0, len(r.mcpSessions))
	for serverID, session := range r.mcpSessions {
		if session != nil {
			sessions = append(sessions, session)
		}
		delete(r.mcpSessions, serverID)
	}
	now := time.Now().UTC()
	for index := range r.mcpSubscriptions {
		switch r.mcpSubscriptions[index].State {
		case "active", "subscribing":
			r.mcpSubscriptions[index].State = "inactive"
			r.mcpSubscriptions[index].UpdatedAt = now
		}
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() {
		for _, session := range sessions {
			session.stop()
		}
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (r *Runtime) mcpSessionContext() context.Context {
	r.mu.RLock()
	ctx := r.shutdownCtx
	r.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *mcpSession) stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	stdin := s.stdin
	cmd := s.cmd
	s.mu.Unlock()
	shutdownMCPCommand(cmd, stdin)
}

func mcpNotificationURI(params map[string]any) string {
	if params == nil {
		return ""
	}
	if uri, _ := params["uri"].(string); uri != "" {
		return uri
	}
	if resource, _ := params["resource"].(map[string]any); resource != nil {
		uri, _ := resource["uri"].(string)
		return uri
	}
	return ""
}

func mcpNotificationOutput(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	data, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	output, truncated := truncateMCPOutput(string(data))
	if truncated {
		return output + "..."
	}
	return output
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
	configDir := filepath.Dir(r.mcpConfigPath)
	for name, server := range config.MCPServers {
		server.Dir = configDir
		config.MCPServers[name] = server
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

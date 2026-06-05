package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"linea/backend/internal/agent"
)

func (a *App) handleAgentCommand(ctx context.Context, input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, ":agent") &&
		trimmed != ":help" &&
		!strings.HasPrefix(trimmed, ":diag") &&
		!strings.HasPrefix(trimmed, ":symbols") &&
		!strings.HasPrefix(trimmed, ":refs") &&
		!strings.HasPrefix(trimmed, ":mcp") &&
		!strings.HasPrefix(trimmed, ":subagent") &&
		!strings.HasPrefix(trimmed, ":search ") &&
		!strings.HasPrefix(trimmed, ":read ") &&
		!strings.HasPrefix(trimmed, ":loop ") &&
		!strings.HasPrefix(trimmed, ":check ") &&
		!strings.HasPrefix(trimmed, ":approve ") &&
		!strings.HasPrefix(trimmed, ":run ") &&
		!strings.HasPrefix(trimmed, ":hook ") &&
		!strings.HasPrefix(trimmed, ":skill ") &&
		!strings.HasPrefix(trimmed, ":proposal") {
		return "", false
	}
	if a.agent == nil {
		return "Agent tools are not available.", true
	}
	output, err := a.runAgentCommand(ctx, trimmed)
	if err != nil {
		return err.Error(), true
	}
	return output, true
}

func (a *App) runAgentCommand(ctx context.Context, input string) (string, error) {
	switch {
	case input == ":help":
		return agentHelp(), nil
	case input == ":agent" || input == ":agent status":
		status := a.agent.Status(ctx)
		return fmt.Sprintf("Agent %s. Tools %d. Workspace %s. Subagents %d. MCP %d/%d.", status.RunSummary.State, len(status.Tools), onOff(status.WorkspaceRoot != ""), len(status.Subagents), len(status.MCPServers), len(status.MCPTools)), nil
	case input == ":diag":
		diagnostics, err := a.agent.ListDiagnostics(ctx)
		if err != nil {
			return "", err
		}
		if len(diagnostics) == 0 {
			return "No diagnostics.", nil
		}
		return formatDiagnostics(diagnostics), nil
	case input == ":symbols" || strings.HasPrefix(input, ":symbols "):
		query := strings.TrimSpace(strings.TrimPrefix(input, ":symbols"))
		symbols, err := a.agent.ListSymbols(ctx, query)
		if err != nil {
			return "", err
		}
		return formatSymbols(symbols), nil
	case strings.HasPrefix(input, ":refs "):
		query := strings.TrimSpace(strings.TrimPrefix(input, ":refs "))
		references, err := a.agent.ListReferences(ctx, query)
		if err != nil {
			return "", err
		}
		return formatReferences(references), nil
	case input == ":mcp":
		status := a.agent.Status(ctx)
		return formatMCP(status.MCPServers, status.MCPTools), nil
	case strings.HasPrefix(input, ":mcp call "):
		toolID, rawArgs := splitIDAndRest(strings.TrimSpace(strings.TrimPrefix(input, ":mcp call ")))
		args, err := parseMCPArguments(rawArgs)
		if err != nil {
			return "", err
		}
		call, err := a.agent.CallMCPTool(ctx, agent.MCPCallInput{ToolID: toolID, Arguments: args})
		if err != nil {
			return "", err
		}
		return formatMCPCall(call), nil
	case input == ":subagent":
		return formatSubagents(a.agent.Status(ctx).Subagents), nil
	case strings.HasPrefix(input, ":subagent "):
		id, query := splitIDAndRest(strings.TrimSpace(strings.TrimPrefix(input, ":subagent ")))
		run, err := a.agent.RunSubagent(ctx, id, agent.SubagentRunInput{Goal: query, Query: query})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Subagent %s: %s · %s", run.SubagentID, run.State, run.Summary), nil
	case strings.HasPrefix(input, ":search "):
		query := strings.TrimSpace(strings.TrimPrefix(input, ":search "))
		results, err := a.agent.SearchFiles(ctx, query)
		if err != nil {
			return "", err
		}
		return formatSearchResults(results), nil
	case strings.HasPrefix(input, ":read "):
		path := strings.TrimSpace(strings.TrimPrefix(input, ":read "))
		file, err := a.agent.ReadFile(ctx, path)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s · %d bytes\n\n```%s\n%s\n```", file.Path, file.Size, languageFromPath(file.Path), strings.TrimRight(file.Content, "\n")), nil
	case strings.HasPrefix(input, ":loop "):
		value := strings.TrimSpace(strings.TrimPrefix(input, ":loop "))
		if strings.HasPrefix(value, "continue ") {
			loop, err := a.agent.ContinueAgentLoop(ctx, strings.TrimSpace(strings.TrimPrefix(value, "continue ")), agent.AgentLoopContinueInput{})
			if err != nil {
				return "", err
			}
			return formatAgentLoop(loop), nil
		}
		if strings.HasPrefix(value, "cancel ") {
			loop, err := a.agent.CancelAgentLoop(ctx, strings.TrimSpace(strings.TrimPrefix(value, "cancel ")))
			if err != nil {
				return "", err
			}
			return formatAgentLoop(loop), nil
		}
		loop, err := a.agent.StartAgentLoop(ctx, agent.AgentLoopInput{Goal: value})
		if err != nil {
			return "", err
		}
		return formatAgentLoop(loop), nil
	case strings.HasPrefix(input, ":check "):
		command := strings.TrimSpace(strings.TrimPrefix(input, ":check "))
		check, err := a.agent.CheckCommand(ctx, agent.CommandCheckInput{Command: command})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s · %s", check.Command, check.Reason), nil
	case strings.HasPrefix(input, ":approve "):
		command := strings.TrimSpace(strings.TrimPrefix(input, ":approve "))
		approval, err := a.agent.AddCommandApproval(ctx, agent.CommandApprovalInput{Command: command, State: "approved", Detail: "Approved in TUI."})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Approved %s.", approval.Command), nil
	case strings.HasPrefix(input, ":run "):
		command := strings.TrimSpace(strings.TrimPrefix(input, ":run "))
		approvalID := a.approvedCommandApprovalID(ctx, command)
		if approvalID == "" {
			return "", errors.New("Approve command before running it.")
		}
		run, err := a.agent.RunCommand(ctx, agent.CommandCheckInput{Command: command, ApprovalID: approvalID})
		if err != nil {
			return "", err
		}
		return formatCommandRun(run), nil
	case strings.HasPrefix(input, ":hook "):
		id, command := splitIDAndRest(strings.TrimSpace(strings.TrimPrefix(input, ":hook ")))
		approvalID, err := a.approvalIDForOptionalCommand(ctx, command)
		if err != nil {
			return "", err
		}
		execution, err := a.agent.RunHook(ctx, id, agent.HookExecutionInput{Command: command, ApprovalID: approvalID, Detail: "Run from TUI."})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Hook %s: %s", execution.HookRun.HookID, execution.HookRun.State), nil
	case strings.HasPrefix(input, ":skill "):
		id, command := splitIDAndRest(strings.TrimSpace(strings.TrimPrefix(input, ":skill ")))
		approvalID, err := a.approvalIDForSkillCommand(ctx, id, command)
		if err != nil {
			return "", err
		}
		execution, err := a.agent.RunSkill(ctx, id, agent.SkillExecutionInput{Command: command, ApprovalID: approvalID, Detail: "Run from TUI."})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Skill %s: %s", execution.SkillRun.SkillID, execution.SkillRun.State), nil
	case input == ":proposal" || input == ":proposal list":
		return formatEditProposals(a.agent.ListEditProposals(ctx)), nil
	case strings.HasPrefix(input, ":proposal approve "):
		return a.reviewProposal(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":proposal approve ")), "approved")
	case strings.HasPrefix(input, ":proposal reject "):
		return a.reviewProposal(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":proposal reject ")), "rejected")
	case strings.HasPrefix(input, ":proposal apply "):
		proposal, err := a.agent.ApplyEditProposal(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":proposal apply ")))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Applied %s.", proposal.Path), nil
	default:
		return "", errors.New("Unknown agent command.")
	}
}

func agentHelp() string {
	return strings.Join([]string{
		"Commands:",
		":agent status",
		":diag",
		":symbols [query]",
		":refs <identifier>",
		":search <query>",
		":read <path>",
		":loop <goal>",
		":loop continue <id>",
		":loop cancel <id>",
		":mcp",
		":mcp call <tool-id> [json]",
		":subagent [id] [query]",
		":check <command>",
		":approve <command>",
		":run <command>",
		":hook <id> [command]",
		":skill <id> [command]",
		":proposal list",
	}, "\n")
}

func (a *App) approvalIDForOptionalCommand(ctx context.Context, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", nil
	}
	approvalID := a.approvedCommandApprovalID(ctx, command)
	if approvalID == "" {
		return "", errors.New("Approve command before running it.")
	}
	return approvalID, nil
}

func (a *App) approvalIDForSkillCommand(ctx context.Context, skillID string, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		command = a.defaultSkillCommand(ctx, skillID)
	}
	return a.approvalIDForOptionalCommand(ctx, command)
}

func (a *App) defaultSkillCommand(ctx context.Context, skillID string) string {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return ""
	}
	for _, skill := range a.agent.Status(ctx).Skills {
		if skill.ID == skillID {
			return skill.Command
		}
	}
	return ""
}

func (a *App) approvedCommandApprovalID(ctx context.Context, command string) string {
	command = normalizeAgentCommand(command)
	if command == "" {
		return ""
	}
	for _, approval := range a.agent.ListCommandApprovals(ctx) {
		if approval.State == "approved" && normalizeAgentCommand(approval.Command) == command {
			return approval.ID
		}
	}
	return ""
}

func normalizeAgentCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func (a *App) reviewProposal(ctx context.Context, id string, status string) (string, error) {
	proposal, err := a.agent.ReviewEditProposal(ctx, id, agent.EditProposalReviewInput{Status: status, Detail: "Reviewed in TUI."})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s.", strings.Title(status), proposal.Path), nil
}

func formatDiagnostics(items []agent.Diagnostic) string {
	var b strings.Builder
	for index, item := range items {
		if index >= 8 {
			fmt.Fprintf(&b, "\n...%d more", len(items)-index)
			break
		}
		fmt.Fprintf(&b, "%s:%d:%d %s: %s\n", item.Path, item.Line, item.Column, item.Severity, item.Message)
	}
	return strings.TrimSpace(b.String())
}

func formatSymbols(items []agent.WorkspaceSymbol) string {
	if len(items) == 0 {
		return "No symbols."
	}
	var b strings.Builder
	for index, item := range items {
		if index >= 12 {
			fmt.Fprintf(&b, "\n...%d more", len(items)-index)
			break
		}
		fmt.Fprintf(&b, "%s %s · %s:%d\n", item.Kind, item.Name, item.Path, item.Line)
	}
	return strings.TrimSpace(b.String())
}

func formatReferences(items []agent.WorkspaceReference) string {
	if len(items) == 0 {
		return "No references."
	}
	var b strings.Builder
	for index, item := range items {
		if index >= 12 {
			fmt.Fprintf(&b, "\n...%d more", len(items)-index)
			break
		}
		fmt.Fprintf(&b, "%s:%d %s\n", item.Path, item.Line, item.Text)
	}
	return strings.TrimSpace(b.String())
}

func formatMCP(servers []agent.MCPServer, tools []agent.MCPTool) string {
	if len(servers) == 0 && len(tools) == 0 {
		return "No MCP entries."
	}
	var b strings.Builder
	for _, server := range servers {
		fmt.Fprintf(&b, "Server %s · %s", server.Name, server.State)
		if server.Command != "" {
			fmt.Fprintf(&b, " · %s", server.Command)
		}
		b.WriteByte('\n')
	}
	for _, tool := range tools {
		fmt.Fprintf(&b, "Tool %s/%s · %s\n", tool.ServerName, tool.Name, tool.State)
	}
	return strings.TrimSpace(b.String())
}

func parseMCPArguments(value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, errors.New("MCP arguments must be a JSON object.")
	}
	return out, nil
}

func formatMCPCall(call agent.MCPCall) string {
	if call.Output == "" {
		return fmt.Sprintf("MCP %s: %s", call.ToolID, call.State)
	}
	return fmt.Sprintf("MCP %s: %s\n\n```json\n%s\n```", call.ToolID, call.State, call.Output)
}

func formatSubagents(items []agent.Subagent) string {
	if len(items) == 0 {
		return "No subagents."
	}
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "%s · %s · %s\n", item.ID, item.State, item.Purpose)
	}
	return strings.TrimSpace(b.String())
}

func formatSearchResults(items []agent.SearchResult) string {
	if len(items) == 0 {
		return "No search results."
	}
	var b strings.Builder
	for index, item := range items {
		if index >= 8 {
			fmt.Fprintf(&b, "\n...%d more", len(items)-index)
			break
		}
		fmt.Fprintf(&b, "%s:%d %s\n", item.Path, item.Line, item.Text)
	}
	return strings.TrimSpace(b.String())
}

func formatAgentLoop(loop agent.AgentLoop) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n%s", loop.Goal, loop.State, loop.Summary)
	for _, step := range loop.Steps {
		fmt.Fprintf(&b, "\n- %s: %s", step.Title, step.State)
		if step.Detail != "" {
			fmt.Fprintf(&b, " · %s", step.Detail)
		}
	}
	return b.String()
}

func formatCommandRun(run agent.CommandRun) string {
	output := strings.TrimSpace(run.Output)
	if output == "" {
		return fmt.Sprintf("%s · exit %d", run.Command, run.ExitCode)
	}
	return fmt.Sprintf("%s · exit %d\n\n```text\n%s\n```", run.Command, run.ExitCode, output)
}

func formatEditProposals(items []agent.EditProposal) string {
	if len(items) == 0 {
		return "No edit proposals."
	}
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "%s · %s · %s\n", item.ID, item.Status, item.Path)
	}
	return strings.TrimSpace(b.String())
}

func splitIDAndRest(value string) (string, string) {
	id, rest, ok := strings.Cut(value, " ")
	if !ok {
		return strings.TrimSpace(value), ""
	}
	return strings.TrimSpace(id), strings.TrimSpace(rest)
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func languageFromPath(path string) string {
	_, ext, ok := strings.Cut(strings.TrimSpace(path), ".")
	if !ok {
		return "text"
	}
	switch strings.ToLower(ext) {
	case "go":
		return "go"
	case "md":
		return "markdown"
	case "js", "jsx", "mjs", "cjs":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "html", "htm":
		return "html"
	default:
		return "text"
	}
}

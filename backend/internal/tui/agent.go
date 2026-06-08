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
		!strings.HasPrefix(trimmed, ":trace ") &&
		!strings.HasPrefix(trimmed, ":hook-run ") &&
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
		status.MCPTools = a.agent.ListMCPTools(ctx)
		status.MCPResources = a.agent.ListMCPResources(ctx)
		status.MCPPrompts = a.agent.ListMCPPrompts(ctx)
		return formatMCP(status), nil
	case strings.HasPrefix(input, ":mcp read "):
		call, err := a.agent.ReadMCPResource(ctx, mcpResourceReadInput(strings.TrimSpace(strings.TrimPrefix(input, ":mcp read "))))
		if err != nil {
			return "", err
		}
		return formatMCPCall(call), nil
	case strings.HasPrefix(input, ":mcp subscribe "):
		subscription, err := a.agent.SubscribeMCPResource(ctx, mcpSubscribeInput(strings.TrimSpace(strings.TrimPrefix(input, ":mcp subscribe "))))
		if err != nil {
			return "", err
		}
		return formatMCPSubscription(subscription), nil
	case strings.HasPrefix(input, ":mcp unsubscribe "):
		subscription, err := a.agent.UnsubscribeMCPResource(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":mcp unsubscribe ")))
		if err != nil {
			return "", err
		}
		return formatMCPSubscription(subscription), nil
	case strings.HasPrefix(input, ":mcp prompt "):
		promptID, rawArgs := splitIDAndRest(strings.TrimSpace(strings.TrimPrefix(input, ":mcp prompt ")))
		args, err := parseMCPArguments(rawArgs)
		if err != nil {
			return "", err
		}
		call, err := a.agent.GetMCPPrompt(ctx, agent.MCPPromptGetInput{PromptID: promptID, Arguments: args})
		if err != nil {
			return "", err
		}
		return formatMCPCall(call), nil
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
			loop, err := a.continueAgentLoop(ctx, strings.TrimSpace(strings.TrimPrefix(value, "continue ")))
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
		loop, err := a.startAgentLoop(ctx, value)
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
	case strings.HasPrefix(input, ":trace "):
		event, state, detail := splitThree(strings.TrimSpace(strings.TrimPrefix(input, ":trace ")))
		trace, err := a.agent.AddTrace(ctx, agent.TraceInput{Event: event, State: state, Detail: detail})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Trace %s: %s", trace.Event, trace.State), nil
	case strings.HasPrefix(input, ":hook-run "):
		hookID, state, detail := splitThree(strings.TrimSpace(strings.TrimPrefix(input, ":hook-run ")))
		run, err := a.agent.AddHookRun(ctx, agent.HookRunInput{HookID: hookID, State: state, Detail: detail})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Hook run %s: %s", run.HookID, run.State), nil
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
	case strings.HasPrefix(input, ":proposal create "):
		path, content := splitIDAndRest(strings.TrimSpace(strings.TrimPrefix(input, ":proposal create ")))
		proposal, err := a.agent.ProposeEdit(ctx, agent.EditProposalInput{Path: path, Content: content, Summary: "Created from TUI."})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Proposal %s: %s", proposal.Path, proposal.Status), nil
	case strings.HasPrefix(input, ":proposal approve "):
		return a.reviewProposal(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":proposal approve ")), "approved")
	case strings.HasPrefix(input, ":proposal reject "):
		return a.reviewProposal(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":proposal reject ")), "rejected")
	case strings.HasPrefix(input, ":proposal apply "):
		proposal, err := a.agent.ApplyEditProposal(ctx, strings.TrimSpace(strings.TrimPrefix(input, ":proposal apply ")))
		if err != nil {
			return "", err
		}
		if output, ok := a.continueAppliedAutoLoop(ctx, proposal.ID); ok {
			return fmt.Sprintf("Applied %s.\n\n%s", proposal.Path, output), nil
		}
		return fmt.Sprintf("Applied %s.", proposal.Path), nil
	default:
		return "", errors.New("Unknown agent command.")
	}
}

func (a *App) startAgentLoop(ctx context.Context, value string) (agent.AgentLoop, error) {
	mode := ""
	autoApply := false
	goal := strings.TrimSpace(value)
	if rest, ok := strings.CutPrefix(goal, "auto "); ok {
		mode = "auto"
		autoApply = true
		goal = strings.TrimSpace(rest)
	} else if rest, ok := strings.CutPrefix(goal, "developer "); ok {
		mode = "developer"
		autoApply = true
		goal = strings.TrimSpace(rest)
	} else if rest, ok := strings.CutPrefix(goal, "guided "); ok {
		mode = "guided"
		goal = strings.TrimSpace(rest)
	}
	return a.agent.StartAgentLoop(ctx, agent.AgentLoopInput{Goal: goal, Mode: mode, AutoApply: autoApply})
}

func (a *App) continueAgentLoop(ctx context.Context, id string) (agent.AgentLoop, error) {
	input := agent.AgentLoopContinueInput{}
	for _, loop := range a.agent.ListAgentLoops(ctx) {
		if loop.ID != id || !agentLoopContinuesAutonomously(loop.Mode) || !agentLoopAtLimit(loop) {
			continue
		}
		input.MaxIterations = loop.MaxIterations + 1
		break
	}
	return a.agent.ContinueAgentLoop(ctx, id, input)
}

func (a *App) continueAppliedAutoLoop(ctx context.Context, proposalID string) (string, bool) {
	for _, loop := range a.agent.ListAgentLoops(ctx) {
		if !agentLoopContinuesAutonomously(loop.Mode) || loop.State != "waiting_approval" {
			continue
		}
		for _, step := range loop.Steps {
			if step.Kind == "edit_review" && step.State == "waiting_approval" && step.CreatedID == proposalID {
				continued, err := a.agent.ContinueAgentLoop(ctx, loop.ID, agent.AgentLoopContinueInput{})
				if err != nil {
					return err.Error(), true
				}
				return formatAgentLoop(continued), true
			}
		}
	}
	return "", false
}

func agentLoopAtLimit(loop agent.AgentLoop) bool {
	if loop.MaxIterations <= 0 {
		return false
	}
	for index := len(loop.Steps) - 1; index >= 0; index-- {
		step := loop.Steps[index]
		if step.Kind == "auto_limit" && step.State == "waiting_input" {
			return true
		}
		if step.Kind == "command_run" || step.Kind == "edit_proposal" || step.State == "waiting_approval" {
			return false
		}
	}
	return false
}

func agentLoopContinuesAutonomously(mode string) bool {
	return mode == "auto" || mode == "developer"
}

func agentHelp() string {
	return strings.Join([]string{
		"Commands:",
		":new",
		":agent status",
		":rename <title>",
		":share",
		":delete confirm",
		":attach <path>",
		":diag",
		":symbols [query]",
		":refs <identifier>",
		":search <query>",
		":read <path>",
		":loop <goal>",
		":loop auto <goal>",
		":loop developer <goal>",
		":loop continue <id>",
		":loop cancel <id>",
		":mcp",
		":mcp read <resource-id-or-uri>",
		":mcp subscribe <resource-id-or-uri>",
		":mcp unsubscribe <subscription-id>",
		":mcp prompt <prompt-id> [json]",
		":mcp call <tool-id> [json]",
		":subagent [id] [query]",
		":check <command>",
		":approve <command>",
		":run <command>",
		":trace <event> <state> [detail]",
		":hook-run <id> <state> [detail]",
		":hook <id> [command]",
		":skill <id> [command]",
		":proposal list",
		":proposal create <path> <content>",
		":proposal approve <id>",
		":proposal reject <id>",
		":proposal apply <id>",
		":quit",
	}, "\n")
}

func mcpResourceReadInput(value string) agent.MCPResourceReadInput {
	if looksLikeURI(value) {
		return agent.MCPResourceReadInput{URI: value}
	}
	return agent.MCPResourceReadInput{ResourceID: value}
}

func mcpSubscribeInput(value string) agent.MCPSubscribeInput {
	if looksLikeURI(value) {
		return agent.MCPSubscribeInput{URI: value}
	}
	return agent.MCPSubscribeInput{ResourceID: value}
}

func looksLikeURI(value string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || scheme == "" {
		return false
	}
	for index, char := range scheme {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			continue
		}
		if index > 0 && ((char >= '0' && char <= '9') || char == '+' || char == '-' || char == '.') {
			continue
		}
		return false
	}
	return true
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

func formatMCP(status agent.Status) string {
	if len(status.MCPServers) == 0 && len(status.MCPTools) == 0 && len(status.MCPResources) == 0 && len(status.MCPPrompts) == 0 {
		return "No MCP entries."
	}
	var b strings.Builder
	for _, server := range status.MCPServers {
		fmt.Fprintf(&b, "Server %s · %s", server.Name, server.State)
		if server.Command != "" {
			fmt.Fprintf(&b, " · %s", server.Command)
		}
		b.WriteByte('\n')
	}
	for _, tool := range status.MCPTools {
		fmt.Fprintf(&b, "Tool %s/%s · %s\n", tool.ServerName, tool.Name, tool.State)
	}
	for _, resource := range status.MCPResources {
		fmt.Fprintf(&b, "Resource %s/%s · %s\n", resource.ServerName, resource.Name, resource.URI)
	}
	for _, prompt := range status.MCPPrompts {
		fmt.Fprintf(&b, "Prompt %s/%s · %s\n", prompt.ServerName, prompt.Name, prompt.State)
	}
	for _, subscription := range status.MCPSubscriptions {
		fmt.Fprintf(&b, "Subscription %s · %s · %s\n", subscription.ID, subscription.State, subscription.URI)
	}
	for _, event := range status.MCPEvents {
		fmt.Fprintf(&b, "Event %s · %s · %s\n", event.Method, event.ServerID, event.URI)
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

func formatMCPSubscription(subscription agent.MCPSubscription) string {
	detail := subscription.URI
	if subscription.Error != "" {
		detail += " · " + subscription.Error
	}
	return fmt.Sprintf("MCP subscription %s: %s · %s", subscription.ID, subscription.State, detail)
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
	if next := agentLoopNextAction(loop); next != "" {
		fmt.Fprintf(&b, "\nNext · %s", next)
	}
	for _, step := range loop.Steps {
		fmt.Fprintf(&b, "\n- %s: %s", step.Title, step.State)
		if step.Command != "" {
			fmt.Fprintf(&b, " · %s", step.Command)
		}
		if step.Detail != "" {
			fmt.Fprintf(&b, " · %s", step.Detail)
		}
	}
	return b.String()
}

func agentLoopNextAction(loop agent.AgentLoop) string {
	for index := len(loop.Steps) - 1; index >= 0; index-- {
		step := loop.Steps[index]
		if step.Kind == "edit_review" && step.State == "waiting_approval" {
			return "review or apply proposal"
		}
		if step.Kind == "command_approval" && step.State == "waiting_approval" && step.Command != "" {
			return "approve command"
		}
		if loop.State == "waiting_input" && step.Kind == "auto_limit" && step.State == "waiting_input" {
			return "continue explicitly"
		}
	}
	return ""
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

func splitThree(value string) (string, string, string) {
	first, rest := splitIDAndRest(value)
	second, third := splitIDAndRest(rest)
	return first, second, third
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

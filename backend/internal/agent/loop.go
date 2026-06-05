package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxAgentLoopItems = 25

func (r *Runtime) ListAgentLoops(context.Context) []AgentLoop {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.agentLoops) == 0 {
		return []AgentLoop{}
	}
	return append([]AgentLoop(nil), r.agentLoops...)
}

func (r *Runtime) StartAgentLoop(ctx context.Context, input AgentLoopInput) (AgentLoop, error) {
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return AgentLoop{}, errors.New("Goal is required.")
	}
	now := time.Now().UTC()
	loop := AgentLoop{
		ID:        newTraceID(),
		Goal:      trimRunes(goal, 280),
		State:     "running",
		CreatedAt: now,
		UpdatedAt: now,
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "plan",
		Title:  "Understand request",
		State:  "completed",
		Detail: "Created a bounded local plan.",
	})
	loop = r.runLoopSteps(ctx, loop, input)
	if loop.State == "running" {
		loop.State = "completed"
	}
	loop.UpdatedAt = time.Now().UTC()
	loop.Summary = loopSummary(loop)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentLoops = append([]AgentLoop{loop}, r.agentLoops...)
	if len(r.agentLoops) > maxAgentLoopItems {
		r.agentLoops = r.agentLoops[:maxAgentLoopItems]
	}
	return loop, nil
}

func (r *Runtime) ContinueAgentLoop(ctx context.Context, id string, input AgentLoopContinueInput) (AgentLoop, error) {
	loop, err := r.agentLoopByID(id)
	if err != nil {
		return AgentLoop{}, err
	}
	if loop.State == "canceled" {
		return AgentLoop{}, errors.New("Agent loop is canceled.")
	}
	if loop.State == "completed" {
		return AgentLoop{}, errors.New("Agent loop is already completed.")
	}
	loop.State = "running"
	continued := false
	for _, step := range loop.Steps {
		if step.Kind != "command_approval" || step.State != "waiting_approval" || strings.TrimSpace(step.Command) == "" {
			continue
		}
		approvalID := step.CreatedID
		if err := r.checkCommandApproval(step.Command, approvalID); err != nil {
			approvalID = r.approvedCommandApprovalID(step.Command)
		}
		run, runErr := r.RunCommand(ctx, CommandCheckInput{Command: step.Command, ApprovalID: approvalID})
		detail := fmt.Sprintf("exit %d", run.ExitCode)
		if runErr == nil && run.ExitCode != 0 {
			runErr = fmt.Errorf("command exited with %d", run.ExitCode)
		}
		if runErr != nil {
			detail = runErr.Error()
		}
		loop = appendLoopStep(loop, "command_run", "Run command", "run_command", runErr, detail, step.Command)
		if runErr == nil {
			loop = appendLoopStep(loop, "review_result", "Review result", "run_command", nil, "Command completed successfully.", step.Command)
			if r.WorkspaceEnabled() && shouldReadDiagnostics(strings.ToLower(loop.Goal)) {
				diagnostics, err := r.ListDiagnostics(ctx)
				loop = appendLoopStep(loop, "diagnostics", "Read diagnostics", "diagnostics", err, fmt.Sprintf("%d diagnostic(s) after command", len(diagnostics)), "")
				if err == nil && len(diagnostics) > 0 {
					loop.Steps = append(loop.Steps, AgentLoopStep{
						ID:     newTraceID(),
						Kind:   "diagnostics_review",
						Title:  "Review diagnostics",
						State:  "attention",
						Detail: fmt.Sprintf("%d diagnostic(s) remain.", len(diagnostics)),
						ToolID: "diagnostics",
					})
					loop.State = "attention"
				}
			}
		}
		continued = true
		break
	}
	if !continued {
		loop = r.runLoopSteps(ctx, loop, AgentLoopInput{
			Goal:     loop.Goal,
			Command:  input.Command,
			Query:    input.Query,
			FilePath: input.FilePath,
		})
	}
	if loop.State == "running" {
		loop.State = "completed"
	}
	loop.UpdatedAt = time.Now().UTC()
	loop.Summary = loopSummary(loop)
	r.replaceAgentLoop(loop)
	return loop, nil
}

func (r *Runtime) CancelAgentLoop(_ context.Context, id string) (AgentLoop, error) {
	loop, err := r.agentLoopByID(id)
	if err != nil {
		return AgentLoop{}, err
	}
	if loop.State == "completed" {
		return AgentLoop{}, errors.New("Agent loop is already completed.")
	}
	loop.State = "canceled"
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "cancel",
		Title:  "Cancel loop",
		State:  "completed",
		Detail: "Canceled by user.",
	})
	loop.UpdatedAt = time.Now().UTC()
	loop.Summary = loopSummary(loop)
	r.replaceAgentLoop(loop)
	return loop, nil
}

func (r *Runtime) agentLoopByID(id string) (AgentLoop, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AgentLoop{}, errors.New("Agent loop ID is required.")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, loop := range r.agentLoops {
		if loop.ID == id {
			return loop, nil
		}
	}
	return AgentLoop{}, errors.New("Agent loop was not found.")
}

func (r *Runtime) replaceAgentLoop(loop AgentLoop) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, current := range r.agentLoops {
		if current.ID == loop.ID {
			r.agentLoops[index] = loop
			return
		}
	}
}

func (r *Runtime) runLoopSteps(ctx context.Context, loop AgentLoop, input AgentLoopInput) AgentLoop {
	goalLower := strings.ToLower(loop.Goal)
	if r.WorkspaceEnabled() {
		if shouldReadDiagnostics(goalLower) {
			diagnostics, err := r.ListDiagnostics(ctx)
			loop = appendLoopStep(loop, "diagnostics", "Read diagnostics", "diagnostics", err, fmt.Sprintf("%d diagnostic(s)", len(diagnostics)), "")
		}
		query := loopSearchQuery(input, loop.Goal)
		if query != "" {
			results, err := r.SearchFiles(ctx, query)
			loop = appendLoopStep(loop, "search_files", "Search workspace", "search_files", err, fmt.Sprintf("%d result(s) for %q", len(results), query), "")
		}
		filePath := strings.TrimSpace(input.FilePath)
		if filePath != "" {
			file, err := r.ReadFile(ctx, filePath)
			loop = appendLoopStep(loop, "read_file", "Read file", "read_file", err, fmt.Sprintf("%s · %d bytes", file.Path, file.Size), "")
		}
		if shouldReadSymbols(goalLower) {
			query := loopSymbolQuery(input, loop.Goal)
			symbols, err := r.ListSymbols(ctx, query)
			loop = appendLoopStep(loop, "symbols", "Read symbols", "symbols", err, fmt.Sprintf("%d symbol(s) for %q", len(symbols), query), "")
		}
		if shouldReadReferences(goalLower) {
			query := loopSymbolQuery(input, loop.Goal)
			references, err := r.ListReferences(ctx, query)
			loop = appendLoopStep(loop, "references", "Read references", "references", err, fmt.Sprintf("%d reference(s) for %q", len(references), query), "")
		}
	} else if shouldUseWorkspace(goalLower) || strings.TrimSpace(input.Query) != "" || strings.TrimSpace(input.FilePath) != "" {
		loop = appendLoopStep(loop, "workspace", "Use workspace tools", "workspace", ErrWorkspaceDisabled, "", "")
	}
	if strings.Contains(goalLower, "mcp") {
		servers := r.ListMCPServers(ctx)
		tools := r.ListMCPTools(ctx)
		loop = appendLoopStep(loop, "mcp", "Inspect MCP", "mcp", nil, fmt.Sprintf("%d server(s), %d tool(s)", len(servers), len(tools)), "")
	}
	command := strings.Join(strings.Fields(input.Command), " ")
	if command != "" {
		check, err := r.CheckCommand(ctx, CommandCheckInput{Command: command})
		detail := "blocked"
		if err == nil {
			detail = check.Reason
		}
		loop = appendLoopStep(loop, "command_check", "Check command", "run_command", err, detail, command)
		if err == nil && check.Allowed {
			approval, approvalErr := r.AddCommandApproval(ctx, CommandApprovalInput{
				Command: command,
				State:   "pending",
				Detail:  "Agent loop requested command approval.",
			})
			if approvalErr != nil {
				loop = appendLoopStep(loop, "command_approval", "Request command approval", "run_command", approvalErr, "", command)
			} else {
				step := AgentLoopStep{
					ID:        newTraceID(),
					Kind:      "command_approval",
					Title:     "Request command approval",
					State:     "waiting_approval",
					Detail:    "Approve before running.",
					ToolID:    "run_command",
					Command:   command,
					CreatedID: approval.ID,
				}
				loop.Steps = append(loop.Steps, step)
				loop.State = "waiting_approval"
			}
		}
	} else if mentionsCommand(goalLower) {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "command_approval",
			Title:  "Need command",
			State:  "waiting_input",
			Detail: "Choose an allowlisted command to check.",
			ToolID: "run_command",
		})
		loop.State = "waiting_input"
	}
	if mentionsEdit(goalLower) {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "edit_boundary",
			Title:  "Edit boundary",
			State:  "waiting_approval",
			Detail: "Create or apply edit proposals explicitly.",
			ToolID: "edit_file",
		})
		if loop.State != "waiting_input" {
			loop.State = "waiting_approval"
		}
	}
	return loop
}

func appendLoopStep(loop AgentLoop, kind string, title string, toolID string, err error, detail string, command string) AgentLoop {
	state := "completed"
	if err != nil {
		state = "blocked"
		detail = err.Error()
		if errors.Is(err, ErrWorkspaceDisabled) {
			state = "waiting_input"
		}
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:      newTraceID(),
		Kind:    kind,
		Title:   title,
		State:   state,
		Detail:  detail,
		ToolID:  toolID,
		Command: command,
	})
	if state == "blocked" {
		loop.State = "attention"
	}
	if state == "waiting_input" && loop.State != "attention" {
		loop.State = "waiting_input"
	}
	return loop
}

func loopSearchQuery(input AgentLoopInput, goal string) string {
	query := strings.TrimSpace(input.Query)
	if query != "" {
		return query
	}
	lower := strings.ToLower(goal)
	for _, prefix := range []string{"search ", "find ", "look for "} {
		index := strings.Index(lower, prefix)
		if index >= 0 {
			term := strings.TrimSpace(goal[index+len(prefix):])
			if len([]rune(term)) > 1 {
				return trimRunes(term, 80)
			}
		}
	}
	return ""
}

func loopSymbolQuery(input AgentLoopInput, goal string) string {
	query := strings.TrimSpace(input.Query)
	if query != "" {
		return query
	}
	goal = strings.TrimSpace(goal)
	lower := strings.ToLower(goal)
	for _, prefix := range []string{
		"find definition ",
		"find reference ",
		"find references ",
		"definition ",
		"reference ",
		"references ",
		"navigate ",
		"symbol ",
		"symbols ",
	} {
		if index := strings.Index(lower, prefix); index >= 0 {
			return trimRunes(trimSymbolQueryTerm(goal[index+len(prefix):]), 80)
		}
	}
	return ""
}

func trimSymbolQueryTerm(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, separator := range []string{" and ", " with ", " then ", ",", "."} {
		if index := strings.Index(lower, separator); index >= 0 {
			value = strings.TrimSpace(value[:index])
			lower = strings.ToLower(value)
		}
	}
	return value
}

func loopSummary(loop AgentLoop) string {
	counts := map[string]int{}
	for _, step := range loop.Steps {
		counts[step.State]++
	}
	switch loop.State {
	case "completed":
		return fmt.Sprintf("Completed %d step(s).", counts["completed"])
	case "waiting_approval":
		return "Waiting for explicit approval."
	case "waiting_input":
		return "Waiting for workspace or command input."
	case "canceled":
		return "Canceled."
	default:
		return "Needs attention."
	}
}

func shouldReadDiagnostics(goal string) bool {
	return strings.Contains(goal, "diagnostic") || strings.Contains(goal, "error") || strings.Contains(goal, "test") || strings.Contains(goal, "build")
}

func shouldUseWorkspace(goal string) bool {
	return shouldReadDiagnostics(goal) || shouldReadSymbols(goal) || shouldReadReferences(goal) || strings.Contains(goal, "file") || strings.Contains(goal, "workspace") || strings.Contains(goal, "search") || strings.Contains(goal, "find")
}

func shouldReadSymbols(goal string) bool {
	return strings.Contains(goal, "symbol") || strings.Contains(goal, "navigate") || strings.Contains(goal, "definition")
}

func shouldReadReferences(goal string) bool {
	return strings.Contains(goal, "reference")
}

func mentionsCommand(goal string) bool {
	return strings.Contains(goal, "run ") || strings.Contains(goal, "test") || strings.Contains(goal, "build") || strings.Contains(goal, "check")
}

func mentionsEdit(goal string) bool {
	return strings.Contains(goal, "edit") || strings.Contains(goal, "change") || strings.Contains(goal, "fix") || strings.Contains(goal, "write")
}

func trimRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

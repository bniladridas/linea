package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxAgentLoopItems          = 25
	defaultAutoLoopIterations  = 5
	maxAutoLoopIterationsLimit = 10
)

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
	mode := normalizeAgentLoopMode(input.Mode)
	loop := AgentLoop{
		ID:            newTraceID(),
		Goal:          trimRunes(goal, 280),
		Mode:          mode,
		State:         "running",
		MaxIterations: normalizeAgentLoopIterations(mode, input.MaxIterations),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "plan",
		Title:  "Understand request",
		State:  "completed",
		Detail: loopPlanDetail(mode),
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
	if loop.Mode == "auto" && input.MaxIterations != 0 {
		nextMax := normalizeAgentLoopIterations(loop.Mode, input.MaxIterations)
		if nextMax > loop.MaxIterations {
			loop.MaxIterations = nextMax
		}
	}
	if loop.Mode == "auto" && input.MaxIterations == 0 && autoLoopLimitReached(loop) && hasExplicitLoopContinueInput(input) {
		loop.MaxIterations = normalizeAgentLoopIterations(loop.Mode, loop.MaxIterations+1)
	}
	loop.State = "running"
	var blocked bool
	loop, blocked = r.consumeAppliedEditReviews(loop)
	if blocked {
		loop.UpdatedAt = time.Now().UTC()
		loop.Summary = loopSummary(loop)
		r.replaceAgentLoop(loop)
		return loop, nil
	}
	continued := false
	for index, step := range loop.Steps {
		if step.Kind != "command_approval" || step.State != "waiting_approval" || strings.TrimSpace(step.Command) == "" {
			continue
		}
		loop.Steps[index].State = "completed"
		loop.Steps[index].Detail = "Approval consumed."
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
		if runErr != nil {
			if loop.Mode == "auto" && r.WorkspaceEnabled() && shouldReadDiagnostics(strings.ToLower(loop.Goal)) {
				diagnostics, err := r.ListDiagnostics(ctx)
				loop = appendLoopStep(loop, "diagnostics", "Read diagnostics", "diagnostics", err, fmt.Sprintf("%d diagnostic(s) after failed command", len(diagnostics)), "")
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
					loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{
						Goal:          loop.Goal,
						Diagnostics:   diagnostics,
						Command:       step.Command,
						CommandOutput: strings.TrimSpace(run.Output),
					})
				} else {
					loop = appendRetryStep(loop, loopRetryDetail(loop, "Command failed."))
				}
			} else {
				loop = appendRetryStep(loop, loopRetryDetail(loop, "Command failed."))
			}
		} else {
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
					if loop.Mode == "auto" {
						loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{
							Goal:          loop.Goal,
							Diagnostics:   diagnostics,
							Command:       step.Command,
							CommandOutput: strings.TrimSpace(run.Output),
						})
					} else {
						loop = appendRetryStep(loop, loopRetryDetail(loop, "Diagnostics remain."))
					}
				}
			}
		}
		continued = true
		break
	}
	if !continued {
		loop = r.runLoopSteps(ctx, loop, AgentLoopInput{
			Goal:            loop.Goal,
			Mode:            loop.Mode,
			MaxIterations:   firstNonZero(input.MaxIterations, loop.MaxIterations),
			Command:         input.Command,
			Query:           input.Query,
			FilePath:        input.FilePath,
			ProposalPath:    input.ProposalPath,
			ProposalContent: input.ProposalContent,
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
			if loop.Mode == "auto" {
				loop = r.appendSubagentLoopStep(ctx, loop, "review", SubagentRunInput{Goal: loop.Goal})
			}
			if err == nil && len(diagnostics) > 0 && loop.Mode == "auto" {
				loop.Steps = append(loop.Steps, AgentLoopStep{
					ID:     newTraceID(),
					Kind:   "diagnostics_review",
					Title:  "Review diagnostics",
					State:  "attention",
					Detail: fmt.Sprintf("%d diagnostic(s) found.", len(diagnostics)),
					ToolID: "diagnostics",
				})
				loop.State = "attention"
				loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{Goal: loop.Goal, Diagnostics: diagnostics})
				if loop.State == "waiting_approval" || loop.State == "waiting_input" || loop.State == "attention" {
					return loop
				}
			}
		}
		query := loopSearchQuery(input, loop.Goal)
		if query != "" {
			results, err := r.SearchFiles(ctx, query)
			loop = appendLoopStep(loop, "search_files", "Search workspace", "search_files", err, fmt.Sprintf("%d result(s) for %q", len(results), query), "")
			if loop.Mode == "auto" {
				subagentID := "search"
				if strings.Contains(goalLower, "doc") {
					subagentID = "docs"
				}
				loop = r.appendSubagentLoopStep(ctx, loop, subagentID, SubagentRunInput{Goal: loop.Goal, Query: query})
			}
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
	if loop.Mode == "auto" && autoLoopLimitReached(loop) {
		return appendAutoLimitStep(loop)
	}
	proposalPath := strings.TrimSpace(input.ProposalPath)
	if proposalPath != "" {
		proposal, err := r.ProposeEdit(ctx, EditProposalInput{
			Path:    proposalPath,
			Content: input.ProposalContent,
			Summary: "Agent loop proposal",
		})
		detail := proposalPath
		createdID := ""
		if err == nil {
			detail = proposal.Path
			createdID = proposal.ID
		}
		loop = appendLoopStep(loop, "edit_proposal", "Create edit proposal", "edit_file", err, detail, "")
		if createdID != "" {
			loop.Steps[len(loop.Steps)-1].CreatedID = createdID
		}
		if loop.Mode == "auto" && autoLoopLimitReached(loop) {
			return appendAutoLimitStep(loop)
		}
	} else if mentionsEdit(goalLower) {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "edit_boundary",
			Title:  "Edit boundary",
			State:  "waiting_approval",
			Detail: "Provide proposal path and content before creating an edit proposal.",
			ToolID: "edit_file",
		})
		if loop.State != "waiting_input" {
			loop.State = "waiting_approval"
		}
	}
	command := strings.Join(strings.Fields(input.Command), " ")
	if command == "" && loop.Mode == "auto" && mentionsCommand(goalLower) {
		inferred, detail := r.inferLoopCommand(ctx, loop.Goal)
		if inferred != "" {
			command = inferred
			loop = appendLoopStep(loop, "command_infer", "Choose check command", "run_command", nil, detail, command)
		}
	}
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
				detail := "Approve before running."
				if loop.Mode == "auto" {
					detail = "Approve to let the auto loop continue."
				}
				step := AgentLoopStep{
					ID:        newTraceID(),
					Kind:      "command_approval",
					Title:     "Request command approval",
					State:     "waiting_approval",
					Detail:    detail,
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
	return loop
}

func (r *Runtime) consumeAppliedEditReviews(loop AgentLoop) (AgentLoop, bool) {
	blocked := false
	for index, step := range loop.Steps {
		if step.Kind != "edit_review" || step.State != "waiting_approval" || strings.TrimSpace(step.CreatedID) == "" {
			continue
		}
		proposal, ok := r.editProposalByID(step.CreatedID)
		if !ok {
			loop.Steps[index].State = "blocked"
			loop.Steps[index].Detail = "Edit proposal was not found."
			loop.State = "attention"
			blocked = true
			continue
		}
		switch proposal.Status {
		case "applied":
			loop.Steps[index].State = "completed"
			loop.Steps[index].Detail = "Proposal applied."
		case "rejected":
			loop.Steps[index].State = "completed"
			loop.Steps[index].Detail = "Proposal rejected."
			loop = appendRetryStep(loop, loopRetryDetail(loop, "Proposal rejected."))
			blocked = true
		default:
			loop.State = "waiting_approval"
			blocked = true
		}
	}
	return loop, blocked
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

func (r *Runtime) appendSubagentLoopStep(ctx context.Context, loop AgentLoop, subagentID string, input SubagentRunInput) AgentLoop {
	run, err := r.RunSubagent(ctx, subagentID, input)
	detail := run.Summary
	state := run.State
	createdID := run.ID
	if err != nil {
		detail = err.Error()
		state = "blocked"
	}
	if strings.TrimSpace(detail) == "" {
		detail = subagentID
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:        newTraceID(),
		Kind:      "subagent_run",
		Title:     "Run subagent",
		State:     state,
		Detail:    fmt.Sprintf("%s: %s", subagentID, detail),
		ToolID:    "subagent",
		CreatedID: createdID,
	})
	if state == "blocked" {
		loop.State = "attention"
	}
	return loop
}

func appendRetryStep(loop AgentLoop, detail string) AgentLoop {
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "retry",
		Title:  "Retry",
		State:  "waiting_input",
		Detail: detail,
	})
	loop.State = "attention"
	return loop
}

func appendAutoLimitStep(loop AgentLoop) AgentLoop {
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "auto_limit",
		Title:  "Auto limit",
		State:  "waiting_input",
		Detail: fmt.Sprintf("Reached %d iteration(s). Continue explicitly to keep going.", loop.MaxIterations),
	})
	loop.State = "waiting_input"
	return loop
}

func (r *Runtime) inferLoopCommand(ctx context.Context, goal string) (string, string) {
	if strings.TrimSpace(goal) == "" {
		return "", ""
	}
	root, err := r.workspaceRootPath()
	if err != nil {
		return "", ""
	}
	goalLower := strings.ToLower(goal)
	if command, detail := r.inferPackageCommand(ctx, root, goalLower); command != "" {
		return command, detail
	}
	candidates := []struct {
		command string
		file    string
		terms   []string
	}{
		{command: "make test", file: "Makefile", terms: []string{"test", "check", "fix", "build"}},
		{command: "make check", file: "Makefile", terms: []string{"check", "fix", "build"}},
		{command: "make ui-check-agent", file: "Makefile", terms: []string{"ui", "agent", "frontend"}},
		{command: "make tui-check", file: "Makefile", terms: []string{"tui", "terminal"}},
		{command: "go test ./...", file: "go.mod", terms: []string{"test", "check", "fix"}},
		{command: "go vet ./...", file: "go.mod", terms: []string{"vet", "check"}},
		{command: "npm run build", file: "package.json", terms: []string{"build", "frontend", "ui", "typescript"}},
	}
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			return "", ""
		default:
		}
		if !r.commandAllowed(candidate.command) || !goalMatchesCommandTerms(goalLower, candidate.terms) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, candidate.file)); err == nil {
			return candidate.command, fmt.Sprintf("Inferred from %s and goal.", candidate.file)
		}
	}
	for _, command := range r.allowedCommands() {
		if goalMatchesCommand(command, goalLower) {
			return command, "Inferred from command allowlist and goal."
		}
	}
	return "", ""
}

func (r *Runtime) inferPackageCommand(ctx context.Context, root string, goal string) (string, string) {
	select {
	case <-ctx.Done():
		return "", ""
	default:
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", ""
	}
	var packageFile struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &packageFile); err != nil || len(packageFile.Scripts) == 0 {
		return "", ""
	}
	for _, script := range packageScriptPreference(goal) {
		if strings.TrimSpace(packageFile.Scripts[script]) == "" {
			continue
		}
		for _, command := range packageScriptCommands(script) {
			if r.commandAllowed(command) {
				return command, fmt.Sprintf("Inferred from package.json script %q.", script)
			}
		}
	}
	return "", ""
}

func packageScriptPreference(goal string) []string {
	switch {
	case strings.Contains(goal, "lint"):
		return []string{"lint", "check", "test", "build"}
	case strings.Contains(goal, "type") || strings.Contains(goal, "typescript"):
		return []string{"typecheck", "type-check", "check", "build", "test"}
	case strings.Contains(goal, "test"):
		return []string{"test", "check", "lint", "build"}
	case strings.Contains(goal, "build"):
		return []string{"build", "check", "test", "lint"}
	case strings.Contains(goal, "ui") || strings.Contains(goal, "frontend") || strings.Contains(goal, "react"):
		return []string{"build", "test", "lint", "check"}
	default:
		return []string{"check", "test", "build", "lint"}
	}
}

func packageScriptCommands(script string) []string {
	if script == "test" {
		return []string{"npm test", "npm run test"}
	}
	return []string{fmt.Sprintf("npm run %s", script)}
}

func (r *Runtime) autoProposeEdit(ctx context.Context, loop AgentLoop, request EditPlanRequest) AgentLoop {
	if autoLoopLimitReached(loop) {
		return appendAutoLimitStep(loop)
	}
	planner := r.editPlanner
	if planner == nil {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "plan_edit",
			Title:  "Plan edit",
			State:  "waiting_input",
			Detail: "Auto edit planning is unavailable.",
			ToolID: "edit_file",
		})
		loop.State = "waiting_input"
		return loop
	}
	files, err := r.autoEditContextFiles(ctx, request.Diagnostics)
	if err != nil {
		loop = appendLoopStep(loop, "read_file", "Read fix context", "read_file", err, "", "")
		return loop
	}
	if len(files) == 0 {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "plan_edit",
			Title:  "Plan edit",
			State:  "waiting_input",
			Detail: "No editable diagnostic file was found.",
			ToolID: "edit_file",
		})
		loop.State = "waiting_input"
		return loop
	}
	request.Files = files
	plan, err := planner.PlanEdit(ctx, request)
	if err != nil {
		loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", err, "", "")
		return loop
	}
	plan.Path = strings.TrimSpace(plan.Path)
	if plan.Path == "" {
		loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", errors.New("planner returned no path"), "", "")
		return loop
	}
	if !editPlanPathInFiles(plan.Path, files) {
		loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", errors.New("planner returned a path outside diagnostic context"), "", "")
		return loop
	}
	loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", nil, plan.Path, "")
	proposal, err := r.ProposeEdit(ctx, EditProposalInput{
		Path:    plan.Path,
		Content: plan.Content,
		Summary: firstNonEmpty(strings.TrimSpace(plan.Summary), "Auto loop proposal"),
	})
	detail := plan.Path
	createdID := ""
	if err == nil {
		detail = proposal.Path
		createdID = proposal.ID
	}
	loop = appendLoopStep(loop, "edit_proposal", "Create edit proposal", "edit_file", err, detail, "")
	if createdID != "" {
		loop.Steps[len(loop.Steps)-1].CreatedID = createdID
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:        newTraceID(),
			Kind:      "edit_review",
			Title:     "Review edit proposal",
			State:     "waiting_approval",
			Detail:    "Review and apply explicitly before running more checks.",
			ToolID:    "edit_file",
			CreatedID: createdID,
		})
		loop.State = "waiting_approval"
	}
	return loop
}

func (r *Runtime) autoEditContextFiles(ctx context.Context, diagnostics []Diagnostic) ([]FileResult, error) {
	seen := map[string]bool{}
	files := []FileResult{}
	for _, diagnostic := range diagnostics {
		path := strings.TrimSpace(diagnostic.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		file, err := r.ReadFile(ctx, path)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
		if len(files) >= 3 {
			break
		}
	}
	return files, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func hasExplicitLoopContinueInput(input AgentLoopContinueInput) bool {
	return strings.TrimSpace(input.Command) != "" ||
		strings.TrimSpace(input.Query) != "" ||
		strings.TrimSpace(input.FilePath) != "" ||
		strings.TrimSpace(input.ProposalPath) != ""
}

func editPlanPathInFiles(path string, files []FileResult) bool {
	path = strings.Trim(strings.TrimSpace(path), "/")
	for _, file := range files {
		if strings.Trim(strings.TrimSpace(file.Path), "/") == path {
			return true
		}
	}
	return false
}

func normalizeAgentLoopMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "auto"
	default:
		return "guided"
	}
}

func normalizeAgentLoopIterations(mode string, limit int) int {
	if mode != "auto" {
		return 0
	}
	if limit <= 0 {
		return defaultAutoLoopIterations
	}
	if limit > maxAutoLoopIterationsLimit {
		return maxAutoLoopIterationsLimit
	}
	return limit
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func loopPlanDetail(mode string) string {
	if mode == "auto" {
		return "Created an auto local plan with explicit approval boundaries."
	}
	return "Created a bounded local plan."
}

func loopRetryDetail(loop AgentLoop, reason string) string {
	if loop.Mode == "auto" {
		return reason + " Auto loop paused for the next proposal or approved command."
	}
	return reason + " Provide another proposal or command to continue."
}

func autoLoopLimitReached(loop AgentLoop) bool {
	if loop.Mode != "auto" || loop.MaxIterations <= 0 {
		return false
	}
	iterations := 0
	for _, step := range loop.Steps {
		switch step.Kind {
		case "command_run", "edit_proposal":
			iterations++
		}
	}
	return iterations >= loop.MaxIterations
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

func goalMatchesCommandTerms(goal string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(goal, term) {
			return true
		}
	}
	return false
}

func goalMatchesCommand(command string, goal string) bool {
	command = strings.ToLower(command)
	switch {
	case strings.Contains(goal, "build"):
		return strings.Contains(command, "build") || strings.Contains(command, "check") || strings.Contains(command, "test")
	case strings.Contains(goal, "test"):
		return strings.Contains(command, "test") || strings.Contains(command, "check")
	case strings.Contains(goal, "check"):
		return strings.Contains(command, "check") || strings.Contains(command, "test") || strings.Contains(command, "vet")
	default:
		return false
	}
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
		if loop.Mode == "auto" {
			return fmt.Sprintf("Auto loop completed %d step(s).", counts["completed"])
		}
		return fmt.Sprintf("Completed %d step(s).", counts["completed"])
	case "waiting_approval":
		if loop.Mode == "auto" {
			return "Auto loop waiting for explicit approval."
		}
		return "Waiting for explicit approval."
	case "waiting_input":
		if loop.Mode == "auto" {
			return "Auto loop waiting for input."
		}
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

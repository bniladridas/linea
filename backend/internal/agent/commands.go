package agent

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const (
	commandTimeout   = 30 * time.Second
	maxCommandOutput = 64 * 1024
)

func WithCommandAllowlist(commands []string) func(*Runtime) {
	return func(r *Runtime) {
		r.commands = normalizeCommands(commands)
	}
}

func normalizeCommands(commands []string) []string {
	out := make([]string, 0, len(commands))
	seen := map[string]bool{}
	for _, command := range commands {
		command = strings.Join(strings.Fields(command), " ")
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		out = append(out, command)
	}
	return out
}

func (r *Runtime) ListCommandApprovals(context.Context) []CommandApproval {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.commandApprovals) == 0 {
		return []CommandApproval{}
	}
	return append([]CommandApproval(nil), r.commandApprovals...)
}

func (r *Runtime) AddCommandApproval(_ context.Context, input CommandApprovalInput) (CommandApproval, error) {
	command := strings.Join(strings.Fields(input.Command), " ")
	if command == "" {
		return CommandApproval{}, errors.New("Command is required.")
	}
	state := strings.TrimSpace(input.State)
	if state == "" {
		state = "pending"
	}
	switch state {
	case "pending", "approved", "rejected":
	default:
		return CommandApproval{}, errors.New("Command approval state is invalid.")
	}
	allowed := r.commandAllowed(command)
	if !allowed && state == "approved" {
		return CommandApproval{}, errors.New("Command is not in allowlist.")
	}
	detail := strings.TrimSpace(input.Detail)
	if len([]rune(detail)) > 240 {
		detail = string([]rune(detail)[:240])
	}
	approval := CommandApproval{
		ID:        newTraceID(),
		Command:   command,
		State:     state,
		Detail:    detail,
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandApprovals = append([]CommandApproval{approval}, r.commandApprovals...)
	if len(r.commandApprovals) > 50 {
		r.commandApprovals = r.commandApprovals[:50]
	}
	return approval, nil
}

func (r *Runtime) ListCommandRuns(context.Context) []CommandRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.commandRuns) == 0 {
		return []CommandRun{}
	}
	return append([]CommandRun(nil), r.commandRuns...)
}

func (r *Runtime) RunCommand(ctx context.Context, input CommandCheckInput) (CommandRun, error) {
	check, err := r.CheckCommand(ctx, input)
	if err != nil {
		return CommandRun{}, err
	}
	if err := r.checkCommandApproval(check.Command, check.ApprovalID); err != nil {
		return CommandRun{}, err
	}
	if !check.Allowed {
		return CommandRun{}, errors.New("command is not in allowlist")
	}
	root, err := r.workspaceRootPath()
	if err != nil {
		return CommandRun{}, err
	}
	args := strings.Fields(check.Command)
	if len(args) == 0 {
		return CommandRun{}, errors.New("Command is required.")
	}
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	if runCtx.Err() == context.DeadlineExceeded {
		exitCode = 124
	}
	content := output.String()
	truncated := false
	if len(content) > maxCommandOutput {
		content = content[:maxCommandOutput]
		truncated = true
	}
	run := CommandRun{
		ID:        newTraceID(),
		Command:   check.Command,
		ExitCode:  exitCode,
		Output:    content,
		Truncated: truncated,
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandRuns = append([]CommandRun{run}, r.commandRuns...)
	if len(r.commandRuns) > 50 {
		r.commandRuns = r.commandRuns[:50]
	}
	return run, nil
}

func (r *Runtime) commandAllowed(command string) bool {
	for _, item := range r.commands {
		if command == item {
			return true
		}
	}
	return false
}

func (r *Runtime) commandApproval(id string) (CommandApproval, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CommandApproval{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, approval := range r.commandApprovals {
		if approval.ID == id {
			return approval, true
		}
	}
	return CommandApproval{}, false
}

func (r *Runtime) checkCommandApproval(command string, approvalID string) error {
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return nil
	}
	approval, ok := r.commandApproval(approvalID)
	if !ok {
		return errors.New("Command approval was not found.")
	}
	if approval.Command != command {
		return errors.New("Command approval does not match command.")
	}
	if approval.State != "approved" {
		return errors.New("Command approval is not approved.")
	}
	return nil
}

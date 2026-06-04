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

func (r *Runtime) ListCommandRuns(context.Context) []CommandRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]CommandRun(nil), r.commandRuns...)
}

func (r *Runtime) RunCommand(ctx context.Context, input CommandCheckInput) (CommandRun, error) {
	check, err := r.CheckCommand(ctx, input)
	if err != nil {
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

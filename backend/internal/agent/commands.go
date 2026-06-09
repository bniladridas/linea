package agent

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

var (
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
		ID:          newTraceID(),
		Command:     command,
		State:       state,
		Category:    commandCategory(command),
		Destructive: commandDestructive(command),
		Detail:      detail,
		CreatedAt:   time.Now().UTC(),
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
	if strings.TrimSpace(input.ApprovalID) == "" {
		return CommandRun{}, errors.New("Command approval is required.")
	}
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
	return r.runCheckedCommand(ctx, check.Command)
}

func (r *Runtime) runCheckedCommand(ctx context.Context, command string) (CommandRun, error) {
	root, err := r.workspaceRootPath()
	if err != nil {
		return CommandRun{}, err
	}
	args := strings.Fields(command)
	if len(args) == 0 {
		return CommandRun{}, errors.New("Command is required.")
	}
	timeout := commandTimeout
	outputLimit := maxCommandOutput
	if r.unrestricted {
		timeout = 5 * time.Minute
		outputLimit = 1024 * 1024
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
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
	content := redactSecrets(output.String())
	truncated := false
	if len(content) > outputLimit {
		content = content[:outputLimit]
		truncated = true
	}
	run := CommandRun{
		ID:        newTraceID(),
		Command:   command,
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

func (r *Runtime) allowedCommands() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.commands...)
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

func (r *Runtime) approvedCommandApprovalID(command string) string {
	command = strings.Join(strings.Fields(command), " ")
	if command == "" {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, approval := range r.commandApprovals {
		if approval.Command == command && approval.State == "approved" {
			return approval.ID
		}
	}
	return ""
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

// commandCategory classifies a command as read, write, or inspect based on
// the leading executable name. Unknown commands default to "unknown".
func commandCategory(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "unknown"
	}
	base := fields[0]

	readCommands := map[string]bool{
		"cat": true, "head": true, "tail": true, "less": true,
		"more": true, "wc": true, "file": true, "stat": true,
		"find": true, "ls": true, "tree": true, "du": true,
		"df": true, "which": true, "whoami": true, "id": true,
		"echo": true, "printf": true, "date": true, "uname": true,
		"env": true, "printenv": true, "pwd": true, "hostname": true,
	}
	inspectCommands := map[string]bool{
		"grep": true, "rg": true, "ag": true, "ack": true,
		"diff": true, "git": true, "go": true, "npm": true,
		"node": true, "python": true, "python3": true,
		"make": true, "cargo": true, "rustc": true,
		"gcc": true, "g++": true, "clang": true,
		"test": true, "[": true, "true": true, "false": true,
	}
	writeCommands := map[string]bool{
		"cp": true, "mv": true, "mkdir": true, "touch": true,
		"tee": true, "sed": true, "awk": true, "patch": true,
		"install": true, "ln": true, "chmod": true, "chown": true,
	}

	switch {
	case readCommands[base]:
		return "read"
	case inspectCommands[base]:
		// git subcommands can be read or write
		if base == "git" && len(fields) > 1 {
			return gitSubcommandCategory(fields[1])
		}
		if base == "go" && len(fields) > 1 {
			return goSubcommandCategory(fields[1])
		}
		if base == "npm" && len(fields) > 1 {
			return npmSubcommandCategory(fields[1])
		}
		return "inspect"
	case writeCommands[base]:
		return "write"
	default:
		return "unknown"
	}
}

func gitSubcommandCategory(sub string) string {
	switch sub {
	case "status", "log", "diff", "show", "branch", "tag", "remote", "stash",
		"ls-files", "ls-tree", "rev-parse", "describe", "blame", "shortlog":
		return "read"
	case "add", "commit", "push", "pull", "merge", "rebase", "cherry-pick",
		"checkout", "switch", "restore", "reset", "mv", "rm":
		return "write"
	case "clean", "gc", "prune", "filter-branch":
		return "destructive"
	default:
		return "inspect"
	}
}

func goSubcommandCategory(sub string) string {
	switch sub {
	case "build", "test", "vet", "run", "version", "env", "list", "doc", "fmt":
		return "inspect"
	case "install", "get", "mod":
		return "write"
	case "clean":
		return "destructive"
	default:
		return "inspect"
	}
}

func npmSubcommandCategory(sub string) string {
	switch sub {
	case "ls", "list", "outdated", "view", "info", "search", "help",
		"test", "run", "start", "version", "config", "audit":
		return "inspect"
	case "install", "ci", "update", "uninstall", "link", "rebuild",
		"publish", "init", "pack":
		return "write"
	case "cache":
		return "destructive"
	default:
		return "inspect"
	}
}

// commandDestructive returns true if the command is known to be destructive.
func commandDestructive(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]

	destructiveCommands := map[string]bool{
		"rm": true, "rmdir": true, "shred": true,
		"dd": true, "mkfs": true, "fdisk": true,
		"kill": true, "killall": true, "pkill": true,
		"reboot": true, "shutdown": true, "halt": true,
		"sudo": true,
	}
	if destructiveCommands[base] {
		return true
	}

	// Check specific subcommands
	if base == "git" && len(fields) > 1 {
		return gitSubcommandCategory(fields[1]) == "destructive"
	}
	if base == "go" && len(fields) > 1 {
		return goSubcommandCategory(fields[1]) == "destructive"
	}
	if base == "npm" && len(fields) > 1 {
		return npmSubcommandCategory(fields[1]) == "destructive"
	}

	return false
}

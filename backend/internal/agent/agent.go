package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

type Runtime struct {
	mu               sync.RWMutex
	rulesPath        string
	traces           []Trace
	hookRuns         []HookRun
	skillRuns        []SkillRun
	editProposals    []EditProposal
	commandApprovals []CommandApproval
	commandChecks    []CommandCheck
	commandRuns      []CommandRun
	workspaceRoot    string
	skillsDir        string
	mcpConfigPath    string
	commands         []string
}

type Status struct {
	Mode             string            `json:"mode"`
	Rules            RuleSet           `json:"rules"`
	Tools            []Tool            `json:"tools"`
	Hooks            []Hook            `json:"hooks"`
	Skills           []Skill           `json:"skills"`
	MCPServers       []MCPServer       `json:"mcpServers"`
	Boundaries       []string          `json:"boundaries"`
	Next             []string          `json:"next"`
	TraceEvents      []Trace           `json:"traceEvents"`
	RunSummary       RunSummary        `json:"runSummary"`
	HookRuns         []HookRun         `json:"hookRuns"`
	SkillRuns        []SkillRun        `json:"skillRuns"`
	CommandApprovals []CommandApproval `json:"commandApprovals"`
	CommandChecks    []CommandCheck    `json:"commandChecks"`
	CommandRuns      []CommandRun      `json:"commandRuns"`
}

type RuleSet struct {
	Source  string   `json:"source"`
	Loaded  bool     `json:"loaded"`
	Summary []string `json:"summary"`
}

type Tool struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Access   string `json:"access"`
	Approval string `json:"approval"`
}

type Hook struct {
	ID    string `json:"id"`
	Event string `json:"event"`
	State string `json:"state"`
}

type HookRun struct {
	ID        string    `json:"id"`
	HookID    string    `json:"hookId"`
	State     string    `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type HookRunInput struct {
	HookID string `json:"hookId"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type HookExecutionInput struct {
	Command string `json:"command,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type HookExecution struct {
	HookRun    HookRun     `json:"hookRun"`
	CommandRun *CommandRun `json:"commandRun,omitempty"`
}

type Skill struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Command string `json:"command,omitempty"`
}

type SkillRun struct {
	ID        string    `json:"id"`
	SkillID   string    `json:"skillId"`
	State     string    `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type SkillExecutionInput struct {
	Command string `json:"command,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type SkillExecution struct {
	SkillRun   SkillRun    `json:"skillRun"`
	CommandRun *CommandRun `json:"commandRun,omitempty"`
}

type MCPServer struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	State   string   `json:"state"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	EnvKeys []string `json:"envKeys,omitempty"`
}

type CommandCheck struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	ApprovalID string    `json:"approvalId,omitempty"`
	Allowed    bool      `json:"allowed"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"createdAt"`
}

type CommandCheckInput struct {
	Command    string `json:"command"`
	ApprovalID string `json:"approvalId,omitempty"`
}

type CommandApproval struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	State     string    `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type CommandApprovalInput struct {
	Command string `json:"command"`
	State   string `json:"state,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type CommandRun struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	ExitCode  int       `json:"exitCode"`
	Output    string    `json:"output"`
	Truncated bool      `json:"truncated"`
	CreatedAt time.Time `json:"createdAt"`
}

type Trace struct {
	ID        string    `json:"id"`
	Event     string    `json:"event"`
	State     string    `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type TraceInput struct {
	Event  string `json:"event"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type RunSummary struct {
	State            string    `json:"state"`
	TraceEvents      int       `json:"traceEvents"`
	HookRuns         int       `json:"hookRuns"`
	SkillRuns        int       `json:"skillRuns"`
	CommandApprovals int       `json:"commandApprovals"`
	CommandChecks    int       `json:"commandChecks"`
	CommandRuns      int       `json:"commandRuns"`
	EditProposals    int       `json:"editProposals"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func NewRuntime(rulesPath string, options ...func(*Runtime)) *Runtime {
	if strings.TrimSpace(rulesPath) == "" {
		rulesPath = "AGENTS.md"
	}
	runtime := &Runtime{rulesPath: rulesPath}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	return runtime
}

func (r *Runtime) Status(ctx context.Context) Status {
	return Status{
		Mode:             "local",
		Rules:            r.loadRules(ctx),
		Tools:            r.tools(),
		Hooks:            defaultHooks(),
		Skills:           r.skills(ctx),
		MCPServers:       r.mcpServers(ctx),
		Boundaries:       defaultBoundaries(),
		Next:             defaultNext(),
		TraceEvents:      r.statusTraces(),
		RunSummary:       r.RunSummary(ctx),
		HookRuns:         r.statusHookRuns(),
		SkillRuns:        r.statusSkillRuns(),
		CommandApprovals: r.statusCommandApprovals(),
		CommandChecks:    r.statusCommandChecks(),
		CommandRuns:      r.statusCommandRuns(),
	}
}

func (r *Runtime) RunSummary(context.Context) RunSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := "ready"
	var updatedAt time.Time
	for _, run := range r.commandRuns {
		if run.CreatedAt.After(updatedAt) {
			updatedAt = run.CreatedAt
		}
		if run.ExitCode != 0 {
			state = "attention"
		}
	}
	for _, check := range r.commandChecks {
		if check.CreatedAt.After(updatedAt) {
			updatedAt = check.CreatedAt
		}
		if !check.Allowed {
			state = "attention"
		}
	}
	for _, proposal := range r.editProposals {
		if proposal.CreatedAt.After(updatedAt) {
			updatedAt = proposal.CreatedAt
		}
		if proposal.ReviewedAt != nil && proposal.ReviewedAt.After(updatedAt) {
			updatedAt = *proposal.ReviewedAt
		}
		if proposal.Status == "rejected" {
			state = "attention"
		}
	}
	for _, trace := range r.traces {
		if trace.CreatedAt.After(updatedAt) {
			updatedAt = trace.CreatedAt
		}
	}
	for _, run := range r.hookRuns {
		if run.CreatedAt.After(updatedAt) {
			updatedAt = run.CreatedAt
		}
		if run.State == "failed" || run.State == "blocked" {
			state = "attention"
		}
	}
	for _, run := range r.skillRuns {
		if run.CreatedAt.After(updatedAt) {
			updatedAt = run.CreatedAt
		}
		if run.State == "failed" || run.State == "blocked" {
			state = "attention"
		}
	}
	for _, approval := range r.commandApprovals {
		if approval.CreatedAt.After(updatedAt) {
			updatedAt = approval.CreatedAt
		}
		if approval.State == "rejected" {
			state = "attention"
		}
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	return RunSummary{
		State:            state,
		TraceEvents:      len(r.traces),
		HookRuns:         len(r.hookRuns),
		SkillRuns:        len(r.skillRuns),
		CommandApprovals: len(r.commandApprovals),
		CommandChecks:    len(r.commandChecks),
		CommandRuns:      len(r.commandRuns),
		EditProposals:    len(r.editProposals),
		UpdatedAt:        updatedAt,
	}
}

func (r *Runtime) ListTraces(context.Context) []Trace {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Trace(nil), r.traces...)
}

func (r *Runtime) AddTrace(_ context.Context, input TraceInput) (Trace, error) {
	event := strings.TrimSpace(input.Event)
	state := strings.TrimSpace(input.State)
	if event == "" || state == "" {
		return Trace{}, errors.New("Trace event and state are required.")
	}
	detail := strings.TrimSpace(input.Detail)
	if len([]rune(detail)) > 240 {
		detail = string([]rune(detail)[:240])
	}
	trace := Trace{
		ID:        newTraceID(),
		Event:     event,
		State:     state,
		Detail:    detail,
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.traces = append([]Trace{trace}, r.traces...)
	if len(r.traces) > 50 {
		r.traces = r.traces[:50]
	}
	return trace, nil
}

func (r *Runtime) ListHookRuns(context.Context) []HookRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]HookRun(nil), r.hookRuns...)
}

func (r *Runtime) AddHookRun(_ context.Context, input HookRunInput) (HookRun, error) {
	hookID := strings.TrimSpace(input.HookID)
	state := strings.TrimSpace(input.State)
	if hookID == "" || state == "" {
		return HookRun{}, errors.New("Hook ID and state are required.")
	}
	if !knownHookID(hookID) {
		return HookRun{}, errors.New("Unknown hook ID.")
	}
	detail := strings.TrimSpace(input.Detail)
	if len([]rune(detail)) > 240 {
		detail = string([]rune(detail)[:240])
	}
	run := HookRun{
		ID:        newTraceID(),
		HookID:    hookID,
		State:     state,
		Detail:    detail,
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hookRuns = append([]HookRun{run}, r.hookRuns...)
	if len(r.hookRuns) > 50 {
		r.hookRuns = r.hookRuns[:50]
	}
	return run, nil
}

func (r *Runtime) RunHook(ctx context.Context, hookID string, input HookExecutionInput) (HookExecution, error) {
	hookID = strings.TrimSpace(hookID)
	if hookID == "" {
		return HookExecution{}, errors.New("Hook ID is required.")
	}
	if !knownHookID(hookID) {
		return HookExecution{}, errors.New("Unknown hook ID.")
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		run, err := r.AddHookRun(ctx, HookRunInput{HookID: hookID, State: "completed", Detail: input.Detail})
		if err != nil {
			return HookExecution{}, err
		}
		return HookExecution{HookRun: run}, nil
	}
	commandRun, err := r.RunCommand(ctx, CommandCheckInput{Command: command})
	state := "completed"
	if err != nil {
		state = "blocked"
	} else if commandRun.ExitCode != 0 {
		state = "failed"
	}
	detail := input.Detail
	if strings.TrimSpace(detail) == "" {
		detail = command
	}
	hookRun, hookErr := r.AddHookRun(ctx, HookRunInput{HookID: hookID, State: state, Detail: detail})
	if hookErr != nil {
		return HookExecution{}, hookErr
	}
	if err != nil {
		return HookExecution{HookRun: hookRun}, err
	}
	return HookExecution{HookRun: hookRun, CommandRun: &commandRun}, nil
}

func (r *Runtime) ListCommandChecks(context.Context) []CommandCheck {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]CommandCheck(nil), r.commandChecks...)
}

func (r *Runtime) CheckCommand(_ context.Context, input CommandCheckInput) (CommandCheck, error) {
	command := strings.Join(strings.Fields(input.Command), " ")
	if command == "" {
		return CommandCheck{}, errors.New("Command is required.")
	}
	approvalID := strings.TrimSpace(input.ApprovalID)
	allowed := r.commandAllowed(command)
	reason := "not in allowlist"
	if allowed {
		reason = "allowed"
	}
	if allowed {
		if err := r.checkCommandApproval(command, approvalID); err != nil {
			allowed = false
			reason = err.Error()
		}
	}
	check := CommandCheck{
		ID:         newTraceID(),
		Command:    command,
		ApprovalID: approvalID,
		Allowed:    allowed,
		Reason:     reason,
		CreatedAt:  time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandChecks = append([]CommandCheck{check}, r.commandChecks...)
	if len(r.commandChecks) > 50 {
		r.commandChecks = r.commandChecks[:50]
	}
	return check, nil
}

func (r *Runtime) statusTraces() []Trace {
	traces := r.ListTraces(context.Background())
	if len(traces) > 5 {
		return traces[:5]
	}
	if len(traces) > 0 {
		return traces
	}
	return []Trace{{
		ID:        "runtime-ready",
		Event:     "agent runtime",
		State:     "ready",
		CreatedAt: time.Now().UTC(),
	}}
}

func (r *Runtime) statusHookRuns() []HookRun {
	runs := r.ListHookRuns(context.Background())
	if len(runs) > 5 {
		return runs[:5]
	}
	return runs
}

func (r *Runtime) ListSkillRuns(context.Context) []SkillRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]SkillRun(nil), r.skillRuns...)
}

func (r *Runtime) statusSkillRuns() []SkillRun {
	runs := r.ListSkillRuns(context.Background())
	if len(runs) > 5 {
		return runs[:5]
	}
	return runs
}

func (r *Runtime) statusCommandApprovals() []CommandApproval {
	approvals := r.ListCommandApprovals(context.Background())
	if len(approvals) > 5 {
		return approvals[:5]
	}
	return approvals
}

func (r *Runtime) statusCommandChecks() []CommandCheck {
	checks := r.ListCommandChecks(context.Background())
	if len(checks) > 5 {
		return checks[:5]
	}
	return checks
}

func (r *Runtime) statusCommandRuns() []CommandRun {
	runs := r.ListCommandRuns(context.Background())
	if len(runs) > 5 {
		return runs[:5]
	}
	return runs
}

func (r *Runtime) loadRules(ctx context.Context) RuleSet {
	select {
	case <-ctx.Done():
		return fallbackRules()
	default:
	}

	data, err := os.ReadFile(r.rulesPath)
	if errors.Is(err, os.ErrNotExist) {
		return fallbackRules()
	}
	if err != nil {
		return RuleSet{Source: r.rulesPath, Loaded: false, Summary: []string{"Rules file could not be read."}}
	}
	summary := summarizeRules(string(data))
	if len(summary) == 0 {
		summary = fallbackRules().Summary
	}
	return RuleSet{Source: r.rulesPath, Loaded: true, Summary: summary}
}

func summarizeRules(content string) []string {
	lines := strings.Split(content, "\n")
	summary := make([]string, 0, 6)
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "local-first") ||
			strings.Contains(lower, "only stop for") ||
			strings.Contains(lower, "never commit secrets") ||
			strings.Contains(lower, "prefer simple") ||
			strings.Contains(lower, "run relevant tests") ||
			strings.Contains(lower, "destructive actions") {
			summary = append(summary, strings.TrimSuffix(line, "."))
		}
		if len(summary) == 6 {
			break
		}
	}
	return summary
}

func fallbackRules() RuleSet {
	return RuleSet{
		Source: "built-in",
		Loaded: true,
		Summary: []string{
			"Local-first",
			"Ask before destructive actions",
			"Use environment variables for secrets",
			"Prefer simple solutions",
			"Run checks after changes",
		},
	}
}

func defaultTools() []Tool {
	return []Tool{
		{ID: "read_file", Name: "Read files", Access: "workspace", Approval: "not required"},
		{ID: "search_files", Name: "Search files", Access: "workspace", Approval: "not required"},
		{ID: "edit_file", Name: "Edit files", Access: "workspace", Approval: "required by boundary"},
		{ID: "run_command", Name: "Run commands", Access: "allowlist", Approval: "required by boundary"},
		{ID: "diagnostics", Name: "Read diagnostics", Access: "workspace", Approval: "not required"},
	}
}

func (r *Runtime) tools() []Tool {
	tools := defaultTools()
	for index := range tools {
		if tools[index].Access == "workspace" && !r.WorkspaceEnabled() {
			tools[index].Access = "off"
			tools[index].Approval = "workspace not set"
		}
		if tools[index].ID == "run_command" {
			switch {
			case !r.WorkspaceEnabled():
				tools[index].Access = "off"
				tools[index].Approval = "workspace not set"
			case len(r.commands) == 0:
				tools[index].Access = "off"
				tools[index].Approval = "allowlist empty"
			}
		}
	}
	return tools
}

func defaultHooks() []Hook {
	return []Hook{
		{ID: "before_tool", Event: "Before tool calls", State: "planned"},
		{ID: "after_edit", Event: "After file edits", State: "planned"},
		{ID: "before_commit", Event: "Before commits", State: "planned"},
		{ID: "after_check", Event: "After checks", State: "planned"},
	}
}

func knownHookID(id string) bool {
	for _, hook := range defaultHooks() {
		if hook.ID == id {
			return true
		}
	}
	return false
}

func defaultSkills() []Skill {
	return []Skill{
		{ID: "debug_test", Name: "Debug failing test", State: "planned"},
		{ID: "review_change", Name: "Review change", State: "planned"},
		{ID: "update_docs", Name: "Update docs", State: "planned"},
	}
}

func defaultBoundaries() []string {
	return []string{
		"No destructive action without approval",
		"No billing action",
		"No secrets in logs or commits",
		"No broad system access",
		"No background autonomous jobs",
	}
}

func defaultNext() []string {
	return []string{
		"Add MCP tool listing",
	}
}

func newTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	return hex.EncodeToString(b[:])
}

package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Runtime struct {
	mu               sync.RWMutex
	shutdownCtx      context.Context
	shutdownCancel   context.CancelFunc
	rulesPath        string
	editPlanner      EditPlanner
	traces           []Trace
	hookRuns         []HookRun
	skillRuns        []SkillRun
	subagentRuns     []SubagentRun
	subagentPlans    []SubagentPlanRun
	agentLoops       []AgentLoop
	agentPreviews    []AgentPreview
	appSessions      []AppSession
	mcpCalls         []MCPCall
	mcpSubscriptions []MCPSubscription
	mcpEvents        []MCPEvent
	mcpSessions      map[string]*mcpSession
	mcpListeners     map[string]func(MCPEvent)
	editProposals    []EditProposal
	commandApprovals []CommandApproval
	commandChecks    []CommandCheck
	commandRuns      []CommandRun
	workspaceRoot    string
	developerMode    bool
	workspaceTrust   string
	lspCommand       string
	skillsDir        string
	mcpConfigPath    string
	commands             []string
	autoApproveCategories []string
	auditLogMu           sync.Mutex
	auditLogPath         string
	activeProvider       ProviderInfo
	unrestricted         bool
	backgroundJobs       []BackgroundJob
	backgroundCancel     context.CancelFunc
}

type ProviderInfo struct {
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
	Role  string `json:"role,omitempty"`
}

func (r *Runtime) SetActiveProvider(info ProviderInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activeProvider = info
	r.traces = append([]Trace{{
		ID:        newTraceID(),
		Event:     "provider",
		State:     info.Name + " · " + info.Model,
		Detail:    info.Role,
		CreatedAt: time.Now().UTC(),
	}}, r.traces...)
	if len(r.traces) > 50 {
		r.traces = r.traces[:50]
	}
}

func (r *Runtime) SetUnrestricted(unrestricted bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unrestricted = unrestricted
}

func (r *Runtime) activeProviderInfo() ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeProvider
}

type EditPlanner interface {
	PlanEdit(context.Context, EditPlanRequest) (EditPlan, error)
}

type EditPlanRequest struct {
	Goal          string       `json:"goal"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
	Command       string       `json:"command,omitempty"`
	CommandOutput string       `json:"commandOutput,omitempty"`
	Files         []FileResult `json:"files,omitempty"`
}

type EditPlan struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Summary string `json:"summary,omitempty"`
}

type Status struct {
	Mode             string            `json:"mode"`
	WorkspaceRoot    string            `json:"workspaceRoot,omitempty"`
	DeveloperMode    bool              `json:"developerMode"`
	WorkspaceTrust   string            `json:"workspaceTrust,omitempty"`
	Rules            RuleSet           `json:"rules"`
	Tools            []Tool            `json:"tools"`
	Hooks            []Hook            `json:"hooks"`
	Skills           []Skill           `json:"skills"`
	Subagents        []Subagent        `json:"subagents"`
	MCPState         string            `json:"mcpState"`
	MCPServers       []MCPServer       `json:"mcpServers"`
	MCPTools         []MCPTool         `json:"mcpTools"`
	MCPResources     []MCPResource     `json:"mcpResources"`
	MCPPrompts       []MCPPrompt       `json:"mcpPrompts"`
	MCPCalls         []MCPCall         `json:"mcpCalls"`
	MCPSubscriptions []MCPSubscription `json:"mcpSubscriptions"`
	MCPEvents        []MCPEvent        `json:"mcpEvents"`
	Boundaries       []string          `json:"boundaries"`
	Next             []string          `json:"next"`
	TraceEvents      []Trace           `json:"traceEvents"`
	RunSummary       RunSummary        `json:"runSummary"`
	HookRuns         []HookRun         `json:"hookRuns"`
	SkillRuns        []SkillRun        `json:"skillRuns"`
	SubagentRuns     []SubagentRun     `json:"subagentRuns"`
	SubagentPlans    []SubagentPlanRun `json:"subagentPlans"`
	AgentLoops       []AgentLoop       `json:"agentLoops"`
	CommandApprovals []CommandApproval `json:"commandApprovals"`
	CommandChecks    []CommandCheck    `json:"commandChecks"`
	CommandRuns      []CommandRun      `json:"commandRuns"`
	Providers        []ProviderInfo  `json:"providers,omitempty"`
	Unrestricted    bool              `json:"unrestricted"`
	BackgroundJobs  []BackgroundJob   `json:"backgroundJobs"`
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
	Command    string `json:"command,omitempty"`
	ApprovalID string `json:"approvalId,omitempty"`
	Detail     string `json:"detail,omitempty"`
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
	Command    string `json:"command,omitempty"`
	ApprovalID string `json:"approvalId,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type SkillExecution struct {
	SkillRun   SkillRun    `json:"skillRun"`
	CommandRun *CommandRun `json:"commandRun,omitempty"`
}

type AgentLoop struct {
	ID            string          `json:"id"`
	Goal          string          `json:"goal"`
	Mode          string          `json:"mode"`
	State         string          `json:"state"`
	MaxIterations int             `json:"maxIterations,omitempty"`
	AutoApply     bool            `json:"autoApply,omitempty"`
	SessionID     string          `json:"sessionId,omitempty"`
	WorkspaceRoot string          `json:"workspaceRoot,omitempty"`
	PreviewURL    string          `json:"previewUrl,omitempty"`
	Steps         []AgentLoopStep `json:"steps"`
	Summary       string          `json:"summary"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type AgentLoopStep struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	Command   string `json:"command,omitempty"`
	CreatedID string `json:"createdId,omitempty"`
}

type AgentLoopInput struct {
	Goal            string `json:"goal"`
	Mode            string `json:"mode,omitempty"`
	MaxIterations   int    `json:"maxIterations,omitempty"`
	AutoApply       bool   `json:"autoApply,omitempty"`
	TempWorkspace   bool   `json:"tempWorkspace,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	Command         string `json:"command,omitempty"`
	Query           string `json:"query,omitempty"`
	FilePath        string `json:"filePath,omitempty"`
	ProposalPath    string `json:"proposalPath,omitempty"`
	ProposalContent string `json:"proposalContent,omitempty"`
}

type AgentLoopContinueInput struct {
	Command         string `json:"command,omitempty"`
	MaxIterations   int    `json:"maxIterations,omitempty"`
	AutoApply       bool   `json:"autoApply,omitempty"`
	Query           string `json:"query,omitempty"`
	FilePath        string `json:"filePath,omitempty"`
	ProposalPath    string `json:"proposalPath,omitempty"`
	ProposalContent string `json:"proposalContent,omitempty"`
}

type AgentPreview struct {
	ID        string    `json:"id"`
	LoopID    string    `json:"loopId"`
	SessionID string    `json:"sessionId,omitempty"`
	Entry     string    `json:"entry"`
	URL       string    `json:"url"`
	Root      string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

type PreviewFile struct {
	Path        string
	ContentType string
	Content     []byte
}

type AppSession struct {
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type BackgroundJob struct {
	ID        string    `json:"id"`
	LoopID    string    `json:"loopId"`
	Goal      string    `json:"goal"`
	State     string    `json:"state"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type BackgroundJobInput struct {
	Goal          string `json:"goal"`
	Mode          string `json:"mode,omitempty"`
	MaxIterations int    `json:"maxIterations,omitempty"`
	AutoApply     bool   `json:"autoApply,omitempty"`
}

type Subagent struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Purpose string   `json:"purpose"`
	State   string   `json:"state"`
	Tools   []string `json:"tools"`
}

type SubagentRun struct {
	ID         string    `json:"id"`
	SubagentID string    `json:"subagentId"`
	State      string    `json:"state"`
	Summary    string    `json:"summary"`
	CreatedAt  time.Time `json:"createdAt"`
}

type SubagentRunInput struct {
	Goal  string `json:"goal,omitempty"`
	Query string `json:"query,omitempty"`
}

type SubagentPlanInput struct {
	Goal        string   `json:"goal,omitempty"`
	Query       string   `json:"query,omitempty"`
	SubagentIDs []string `json:"subagentIds,omitempty"`
}

type SubagentPlanRun struct {
	ID          string        `json:"id"`
	Goal        string        `json:"goal"`
	State       string        `json:"state"`
	Summary     string        `json:"summary"`
	SubagentIDs []string      `json:"subagentIds"`
	Runs        []SubagentRun `json:"runs"`
	CreatedAt   time.Time     `json:"createdAt"`
}

type WorkspaceSymbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
	Line int    `json:"line"`
}

type WorkspaceReference struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type MCPServer struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	State   string   `json:"state"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	EnvKeys []string `json:"envKeys,omitempty"`
}

type MCPTool struct {
	ID          string `json:"id"`
	ServerID    string `json:"serverId"`
	ServerName  string `json:"serverName"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema string `json:"inputSchema,omitempty"`
	State       string `json:"state"`
}

type MCPResource struct {
	ID          string `json:"id"`
	ServerID    string `json:"serverId"`
	ServerName  string `json:"serverName"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	State       string `json:"state"`
}

type MCPPrompt struct {
	ID          string `json:"id"`
	ServerID    string `json:"serverId"`
	ServerName  string `json:"serverName"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	State       string `json:"state"`
}

type MCPCall struct {
	ID        string    `json:"id"`
	ToolID    string    `json:"toolId"`
	ServerID  string    `json:"serverId"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	Truncated bool      `json:"truncated"`
	CreatedAt time.Time `json:"createdAt"`
}

type MCPSubscription struct {
	ID         string    `json:"id"`
	ServerID   string    `json:"serverId"`
	ServerName string    `json:"serverName"`
	ResourceID string    `json:"resourceId,omitempty"`
	URI        string    `json:"uri"`
	State      string    `json:"state"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type MCPEvent struct {
	ID             string    `json:"id"`
	SubscriptionID string    `json:"subscriptionId,omitempty"`
	ServerID       string    `json:"serverId"`
	URI            string    `json:"uri,omitempty"`
	Method         string    `json:"method"`
	Output         string    `json:"output,omitempty"`
	Error          string    `json:"error,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type MCPCallInput struct {
	ToolID    string         `json:"toolId"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type MCPResourceReadInput struct {
	ResourceID string `json:"resourceId,omitempty"`
	URI        string `json:"uri,omitempty"`
}

type MCPSubscribeInput struct {
	ResourceID string `json:"resourceId,omitempty"`
	URI        string `json:"uri,omitempty"`
}

type MCPPromptGetInput struct {
	PromptID  string         `json:"promptId,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type CommandCheck struct {
	ID          string    `json:"id"`
	Command     string    `json:"command"`
	ApprovalID  string    `json:"approvalId,omitempty"`
	Allowed     bool      `json:"allowed"`
	Category    string    `json:"category,omitempty"`
	Destructive bool      `json:"destructive,omitempty"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CommandCheckInput struct {
	Command    string `json:"command"`
	ApprovalID string `json:"approvalId,omitempty"`
}

type CommandApproval struct {
	ID          string    `json:"id"`
	Command     string    `json:"command"`
	State       string    `json:"state"`
	Category    string    `json:"category,omitempty"`
	Destructive bool      `json:"destructive,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
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
	SubagentRuns     int       `json:"subagentRuns"`
	AgentLoops       int       `json:"agentLoops"`
	MCPCalls         int       `json:"mcpCalls"`
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
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	runtime := &Runtime{
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
		rulesPath:      rulesPath,
		mcpSessions:    map[string]*mcpSession{},
		mcpListeners:   map[string]func(MCPEvent){},
	}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	if strings.TrimSpace(runtime.lspCommand) == "" {
		runtime.lspCommand = defaultLSPCommand()
	}
	if len(runtime.autoApproveCategories) == 0 {
		if raw := strings.TrimSpace(os.Getenv("LINEA_AUTO_APPROVE_CATEGORIES")); raw != "" {
			for _, cat := range strings.Split(raw, ",") {
				if c := strings.TrimSpace(cat); c != "" {
					runtime.autoApproveCategories = append(runtime.autoApproveCategories, c)
				}
			}
		}
	}
	if runtime.auditLogPath == "" {
		if raw := os.Getenv("LINEA_AUDIT_LOG_PATH"); raw != "" {
			runtime.auditLogPath = strings.TrimSpace(raw)
		} else if _, set := os.LookupEnv("LINEA_AUDIT_LOG_PATH"); !set {
			if cacheDir, err := os.UserCacheDir(); err == nil {
				runtime.auditLogPath = filepath.Join(cacheDir, "linea", "audit.jsonl")
			}
		}
	}
	if runtime.auditLogPath != "" {
		_ = os.MkdirAll(filepath.Dir(runtime.auditLogPath), 0o755)
	}
	runtime.loadPreviewsFromCache()
	runtime.startBackgroundSupervisor()
	return runtime
}

func (r *Runtime) Status(ctx context.Context) Status {
	provider := r.activeProviderInfo()
	providers := []ProviderInfo{}
	if provider.Name != "" {
		providers = append(providers, provider)
	}
	return Status{
		Mode:             "local",
		WorkspaceRoot:    r.WorkspaceRoot(),
		DeveloperMode:    r.developerMode,
		WorkspaceTrust:   r.workspaceTrust,
		Unrestricted:     r.unrestricted,
		Rules:            r.loadRules(ctx),
		Tools:            r.tools(),
		Hooks:            defaultHooks(),
		Skills:           r.skills(ctx),
		Subagents:        defaultSubagents(),
		MCPState:         r.mcpState(ctx),
		MCPServers:       r.mcpServers(ctx),
		MCPTools:         r.statusMCPTools(ctx),
		MCPResources:     r.statusMCPResources(ctx),
		MCPPrompts:       r.statusMCPPrompts(ctx),
		MCPCalls:         r.statusMCPCalls(),
		MCPSubscriptions: r.statusMCPSubscriptions(),
		MCPEvents:        r.statusMCPEvents(),
		Boundaries:       defaultBoundaries(),
		Next:             defaultNext(),
		TraceEvents:      r.statusTraces(),
		RunSummary:       r.RunSummary(ctx),
		HookRuns:         r.statusHookRuns(),
		SkillRuns:        r.statusSkillRuns(),
		SubagentRuns:     r.statusSubagentRuns(),
		SubagentPlans:    r.statusSubagentPlans(),
		AgentLoops:       r.statusAgentLoops(),
		CommandApprovals: r.statusCommandApprovals(),
		CommandChecks:    r.statusCommandChecks(),
		CommandRuns:      r.statusCommandRuns(),
		Providers:        providers,
		BackgroundJobs:    r.listBackgroundJobs(),
	}
}

func (r *Runtime) ListSubagents(context.Context) []Subagent {
	return defaultSubagents()
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
	for _, run := range r.subagentRuns {
		if run.CreatedAt.After(updatedAt) {
			updatedAt = run.CreatedAt
		}
		if run.State == "blocked" || run.State == "waiting_input" {
			state = "attention"
		}
	}
	for _, plan := range r.subagentPlans {
		if plan.CreatedAt.After(updatedAt) {
			updatedAt = plan.CreatedAt
		}
		if plan.State == "blocked" || plan.State == "attention" || plan.State == "waiting_input" {
			state = "attention"
		}
	}
	for _, loop := range r.agentLoops {
		if loop.UpdatedAt.After(updatedAt) {
			updatedAt = loop.UpdatedAt
		}
		if loop.State == "attention" || loop.State == "waiting_approval" || loop.State == "waiting_input" {
			state = "attention"
		}
	}
	for _, call := range r.mcpCalls {
		if call.CreatedAt.After(updatedAt) {
			updatedAt = call.CreatedAt
		}
		if call.State == "failed" || call.State == "blocked" {
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
		SubagentRuns:     len(r.subagentRuns),
		AgentLoops:       len(r.agentLoops),
		MCPCalls:         len(r.mcpCalls),
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
	if len(r.traces) == 0 {
		return []Trace{}
	}
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
	if len(r.hookRuns) == 0 {
		return []HookRun{}
	}
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
	commandRun, err := r.RunCommand(ctx, CommandCheckInput{Command: command, ApprovalID: input.ApprovalID})
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
	if len(r.commandChecks) == 0 {
		return []CommandCheck{}
	}
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
		r.mu.RLock()
		unrestricted := r.unrestricted
		r.mu.RUnlock()
		if unrestricted {
			reason = "auto-approved (unrestricted mode)"
		} else if r.isAutoApproved(command) {
			reason = "auto-approved by category"
		} else if err := r.checkCommandApproval(command, approvalID); err != nil {
			allowed = false
			reason = err.Error()
		}
	}
	check := CommandCheck{
		ID:          newTraceID(),
		Command:     command,
		ApprovalID:  approvalID,
		Allowed:     allowed,
		Category:    commandCategory(command),
		Destructive: commandDestructive(command),
		Reason:      reason,
		CreatedAt:   time.Now().UTC(),
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
	if len(r.skillRuns) == 0 {
		return []SkillRun{}
	}
	return append([]SkillRun(nil), r.skillRuns...)
}

func (r *Runtime) statusSkillRuns() []SkillRun {
	runs := r.ListSkillRuns(context.Background())
	if len(runs) > 5 {
		return runs[:5]
	}
	return runs
}

func (r *Runtime) ListSubagentRuns(context.Context) []SubagentRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.subagentRuns) == 0 {
		return []SubagentRun{}
	}
	return append([]SubagentRun(nil), r.subagentRuns...)
}

func (r *Runtime) statusSubagentRuns() []SubagentRun {
	runs := r.ListSubagentRuns(context.Background())
	if len(runs) > 5 {
		return runs[:5]
	}
	return runs
}

func (r *Runtime) ListMCPCalls(context.Context) []MCPCall {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.mcpCalls) == 0 {
		return []MCPCall{}
	}
	return append([]MCPCall(nil), r.mcpCalls...)
}

func (r *Runtime) statusMCPCalls() []MCPCall {
	calls := r.ListMCPCalls(context.Background())
	if len(calls) > 5 {
		return calls[:5]
	}
	return calls
}

func (r *Runtime) ListMCPSubscriptions(context.Context) []MCPSubscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.mcpSubscriptions) == 0 {
		return []MCPSubscription{}
	}
	return append([]MCPSubscription(nil), r.mcpSubscriptions...)
}

func (r *Runtime) statusMCPSubscriptions() []MCPSubscription {
	items := r.ListMCPSubscriptions(context.Background())
	if len(items) > 5 {
		return items[:5]
	}
	return items
}

func (r *Runtime) ListMCPEvents(context.Context) []MCPEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.mcpEvents) == 0 {
		return []MCPEvent{}
	}
	return append([]MCPEvent(nil), r.mcpEvents...)
}

func (r *Runtime) statusMCPEvents() []MCPEvent {
	items := r.ListMCPEvents(context.Background())
	if len(items) > 5 {
		return items[:5]
	}
	return items
}

func (r *Runtime) statusAgentLoops() []AgentLoop {
	loops := r.ListAgentLoops(context.Background())
	if len(loops) > 5 {
		return loops[:5]
	}
	return loops
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
		{ID: "symbols", Name: "Read symbols", Access: "workspace", Approval: "not required"},
		{ID: "references", Name: "Read references", Access: "workspace", Approval: "not required"},
		{ID: "mcp", Name: "Inspect MCP", Access: "local config", Approval: "not required"},
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

func defaultSubagents() []Subagent {
	return []Subagent{
		{
			ID:      "review",
			Name:    "Review",
			Purpose: "Inspect changes for bugs, regressions, and missing checks.",
			State:   "ready",
			Tools:   []string{"read_file", "search_files", "diagnostics"},
		},
		{
			ID:      "search",
			Name:    "Search",
			Purpose: "Find local context and summarize relevant files.",
			State:   "ready",
			Tools:   []string{"read_file", "search_files"},
		},
		{
			ID:      "test",
			Name:    "Test",
			Purpose: "Run approved checks and report failures.",
			State:   "ready",
			Tools:   []string{"run_command", "diagnostics"},
		},
		{
			ID:      "docs",
			Name:    "Docs",
			Purpose: "Keep documentation aligned with behavior.",
			State:   "ready",
			Tools:   []string{"read_file", "search_files"},
		},
	}
}

func defaultBoundaries() []string {
	return []string{
		"No destructive action without approval",
		"No billing action",
		"No secrets in logs or commits",
		"No broad system access",
	}
}

func defaultNext() []string {
	return []string{}
}

func newTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	return hex.EncodeToString(b[:])
}

func WithAuditLogPath(path string) func(*Runtime) {
	return func(r *Runtime) {
		r.auditLogPath = path
	}
}

func WithAutoApproveCategories(categories []string) func(*Runtime) {
	return func(r *Runtime) {
		r.autoApproveCategories = categories
	}
}

func (r *Runtime) SetAutoApproveCategories(categories []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	r.autoApproveCategories = nil
	for _, cat := range categories {
		c := strings.TrimSpace(cat)
		if c != "" && !seen[c] {
			seen[c] = true
			r.autoApproveCategories = append(r.autoApproveCategories, c)
		}
	}
}

func (r *Runtime) AutoApproveCategories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.autoApproveCategories...)
}

func (r *Runtime) isAutoApproved(command string) bool {
	r.mu.RLock()
	unrestricted := r.unrestricted
	categories := append([]string(nil), r.autoApproveCategories...)
	r.mu.RUnlock()
	if unrestricted {
		return true
	}
	cat := commandCategory(command)
	if cat == "unknown" {
		return false
	}
	for _, allowed := range categories {
		if cat == allowed {
			return true
		}
	}
	return false
}

func (r *Runtime) writeAuditLog(entry any) {
	r.auditLogMu.Lock()
	defer r.auditLogMu.Unlock()
	r.mu.RLock()
	path := r.auditLogPath
	r.mu.RUnlock()
	if path == "" {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	const maxAuditLogSize = 10 * 1024 * 1024 // 10 MB
	if fi, err := os.Stat(path); err == nil && fi.Size() >= maxAuditLogSize {
		os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(data, '\n'))
}

func (r *Runtime) LoadAuditLog() {
	r.mu.RLock()
	path := r.auditLogPath
	r.mu.RUnlock()
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		typ, _ := raw["type"].(string)
		switch typ {
		case "approval":
			var a CommandApproval
			if data, err := json.Marshal(raw["data"]); err == nil {
				if json.Unmarshal(data, &a) == nil {
					r.mu.Lock()
					r.commandApprovals = append([]CommandApproval{a}, r.commandApprovals...)
					if len(r.commandApprovals) > 50 {
						r.commandApprovals = r.commandApprovals[:50]
					}
					r.mu.Unlock()
				}
			}
		case "run":
			var run CommandRun
			if data, err := json.Marshal(raw["data"]); err == nil {
				if json.Unmarshal(data, &run) == nil {
					r.mu.Lock()
					r.commandRuns = append([]CommandRun{run}, r.commandRuns...)
					if len(r.commandRuns) > 50 {
						r.commandRuns = r.commandRuns[:50]
					}
					r.mu.Unlock()
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return
	}
}

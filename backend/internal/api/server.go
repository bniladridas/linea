package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"linea/backend/internal/agent"
	"linea/backend/internal/llm"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
)

type Server struct {
	store          store.Store
	llmClient      Assistant
	searchClient   WebSearcher
	staticFiles    http.FileSystem
	origin         string
	statusProvider StatusProvider
	agentProvider  AgentStatusProvider
	agentRuntime   AgentRuntime
	settingsStore  SettingsStore
}

type Status struct {
	Storage   string           `json:"storage"`
	Search    string           `json:"search"`
	Providers []ProviderStatus `json:"providers"`
}

type ProviderStatus struct {
	Name    string `json:"name"`
	Model   string `json:"model,omitempty"`
	Enabled bool   `json:"enabled"`
	Role    string `json:"role"`
	State   string `json:"state,omitempty"`
	Message string `json:"message,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type StatusProvider func(context.Context) Status

type AgentStatusProvider func(context.Context) agent.Status

type AgentRuntime interface {
	Status(context.Context) agent.Status
	RunSummary(context.Context) agent.RunSummary
	ListSubagents(context.Context) []agent.Subagent
	ListSubagentRuns(context.Context) []agent.SubagentRun
	RunSubagent(context.Context, string, agent.SubagentRunInput) (agent.SubagentRun, error)
	ListMCPServers(context.Context) []agent.MCPServer
	ListMCPTools(context.Context) []agent.MCPTool
	ListMCPCalls(context.Context) []agent.MCPCall
	CallMCPTool(context.Context, agent.MCPCallInput) (agent.MCPCall, error)
	ListTraces(context.Context) []agent.Trace
	AddTrace(context.Context, agent.TraceInput) (agent.Trace, error)
	ListHookRuns(context.Context) []agent.HookRun
	AddHookRun(context.Context, agent.HookRunInput) (agent.HookRun, error)
	RunHook(context.Context, string, agent.HookExecutionInput) (agent.HookExecution, error)
	ListSkillRuns(context.Context) []agent.SkillRun
	RunSkill(context.Context, string, agent.SkillExecutionInput) (agent.SkillExecution, error)
	ListAgentLoops(context.Context) []agent.AgentLoop
	StartAgentLoop(context.Context, agent.AgentLoopInput) (agent.AgentLoop, error)
	ContinueAgentLoop(context.Context, string, agent.AgentLoopContinueInput) (agent.AgentLoop, error)
	CancelAgentLoop(context.Context, string) (agent.AgentLoop, error)
	ListCommandApprovals(context.Context) []agent.CommandApproval
	AddCommandApproval(context.Context, agent.CommandApprovalInput) (agent.CommandApproval, error)
	ListCommandChecks(context.Context) []agent.CommandCheck
	CheckCommand(context.Context, agent.CommandCheckInput) (agent.CommandCheck, error)
	ListCommandRuns(context.Context) []agent.CommandRun
	RunCommand(context.Context, agent.CommandCheckInput) (agent.CommandRun, error)
	ReadFile(context.Context, string) (agent.FileResult, error)
	SearchFiles(context.Context, string) ([]agent.SearchResult, error)
	ListSymbols(context.Context, string) ([]agent.WorkspaceSymbol, error)
	SetWorkspaceRoot(string) (string, error)
	ListDiagnostics(context.Context) ([]agent.Diagnostic, error)
	ListEditProposals(context.Context) []agent.EditProposal
	ProposeEdit(context.Context, agent.EditProposalInput) (agent.EditProposal, error)
	ReviewEditProposal(context.Context, string, agent.EditProposalReviewInput) (agent.EditProposal, error)
	ApplyEditProposal(context.Context, string) (agent.EditProposal, error)
}

type Settings struct {
	Providers []ProviderSetting `json:"providers"`
}

type ProviderSetting struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model,omitempty"`
	Role       string `json:"role"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
}

type SettingsStore interface {
	GetSettings() Settings
	UpdateSettings(Settings) (Settings, error)
}

type Assistant interface {
	GenerateStream(ctx context.Context, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error
}

type WebSearcher interface {
	Search(ctx context.Context, query string) ([]search.Result, error)
}

func NewServer(store store.Store, llmClient Assistant, searchClient WebSearcher, staticFiles http.FileSystem, origin string, status Status) *Server {
	return NewServerWithStatus(store, llmClient, searchClient, staticFiles, origin, func(context.Context) Status {
		return status
	})
}

func NewServerWithStatus(store store.Store, llmClient Assistant, searchClient WebSearcher, staticFiles http.FileSystem, origin string, statusProvider StatusProvider) *Server {
	return NewServerWithStatusAndSettings(store, llmClient, searchClient, staticFiles, origin, statusProvider, nil)
}

func NewServerWithStatusAndSettings(
	store store.Store,
	llmClient Assistant,
	searchClient WebSearcher,
	staticFiles http.FileSystem,
	origin string,
	statusProvider StatusProvider,
	settingsStore SettingsStore,
) *Server {
	return NewServerWithAgentStatus(store, llmClient, searchClient, staticFiles, origin, statusProvider, settingsStore, nil)
}

func NewServerWithAgentStatus(
	store store.Store,
	llmClient Assistant,
	searchClient WebSearcher,
	staticFiles http.FileSystem,
	origin string,
	statusProvider StatusProvider,
	settingsStore SettingsStore,
	agentProvider AgentStatusProvider,
) *Server {
	return &Server{
		store:          store,
		llmClient:      llmClient,
		searchClient:   searchClient,
		staticFiles:    staticFiles,
		origin:         origin,
		statusProvider: statusProvider,
		agentProvider:  agentProvider,
		settingsStore:  settingsStore,
	}
}

func NewServerWithAgentRuntime(
	store store.Store,
	llmClient Assistant,
	searchClient WebSearcher,
	staticFiles http.FileSystem,
	origin string,
	statusProvider StatusProvider,
	settingsStore SettingsStore,
	agentRuntime AgentRuntime,
) *Server {
	return &Server{
		store:          store,
		llmClient:      llmClient,
		searchClient:   searchClient,
		staticFiles:    staticFiles,
		origin:         origin,
		statusProvider: statusProvider,
		agentRuntime:   agentRuntime,
		settingsStore:  settingsStore,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/agent", s.getAgentStatus)
	mux.HandleFunc("GET /api/agent/run-summary", s.getAgentRunSummary)
	mux.HandleFunc("GET /api/agent/runs", s.listAgentRuns)
	mux.HandleFunc("POST /api/agent/runs", s.createAgentRun)
	mux.HandleFunc("GET /api/agent/subagents", s.listAgentSubagents)
	mux.HandleFunc("GET /api/agent/subagent-runs", s.listAgentSubagentRuns)
	mux.HandleFunc("POST /api/agent/subagents/{id}/run", s.runAgentSubagent)
	mux.HandleFunc("GET /api/agent/mcp-servers", s.listAgentMCPServers)
	mux.HandleFunc("GET /api/agent/mcp-tools", s.listAgentMCPTools)
	mux.HandleFunc("GET /api/agent/mcp-calls", s.listAgentMCPCalls)
	mux.HandleFunc("POST /api/agent/mcp-calls", s.callAgentMCPTool)
	mux.HandleFunc("GET /api/agent/traces", s.listAgentTraces)
	mux.HandleFunc("POST /api/agent/traces", s.createAgentTrace)
	mux.HandleFunc("GET /api/agent/hook-runs", s.listAgentHookRuns)
	mux.HandleFunc("POST /api/agent/hook-runs", s.createAgentHookRun)
	mux.HandleFunc("POST /api/agent/hooks/{id}/run", s.runAgentHook)
	mux.HandleFunc("GET /api/agent/skill-runs", s.listAgentSkillRuns)
	mux.HandleFunc("POST /api/agent/skills/{id}/run", s.runAgentSkill)
	mux.HandleFunc("GET /api/agent/loops", s.listAgentLoops)
	mux.HandleFunc("POST /api/agent/loops", s.startAgentLoop)
	mux.HandleFunc("POST /api/agent/loops/{id}/continue", s.continueAgentLoop)
	mux.HandleFunc("POST /api/agent/loops/{id}/cancel", s.cancelAgentLoop)
	mux.HandleFunc("GET /api/agent/command-approvals", s.listAgentCommandApprovals)
	mux.HandleFunc("POST /api/agent/command-approvals", s.createAgentCommandApproval)
	mux.HandleFunc("GET /api/agent/command-checks", s.listAgentCommandChecks)
	mux.HandleFunc("POST /api/agent/command-checks", s.createAgentCommandCheck)
	mux.HandleFunc("GET /api/agent/command-runs", s.listAgentCommandRuns)
	mux.HandleFunc("POST /api/agent/command-runs", s.createAgentCommandRun)
	mux.HandleFunc("GET /api/agent/workspace/file", s.readAgentWorkspaceFile)
	mux.HandleFunc("PATCH /api/agent/workspace", s.updateAgentWorkspace)
	mux.HandleFunc("GET /api/agent/workspace/search", s.searchAgentWorkspace)
	mux.HandleFunc("GET /api/agent/workspace/diagnostics", s.listAgentWorkspaceDiagnostics)
	mux.HandleFunc("GET /api/agent/workspace/symbols", s.listAgentWorkspaceSymbols)
	mux.HandleFunc("GET /api/agent/edit-proposals", s.listAgentEditProposals)
	mux.HandleFunc("POST /api/agent/edit-proposals", s.createAgentEditProposal)
	mux.HandleFunc("PATCH /api/agent/edit-proposals/{id}", s.reviewAgentEditProposal)
	mux.HandleFunc("POST /api/agent/edit-proposals/{id}/apply", s.applyAgentEditProposal)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PATCH /api/settings", s.updateSettings)
	mux.HandleFunc("POST /api/chat/temp", s.createTemporaryMessage)
	mux.HandleFunc("GET /api/conversations", s.listConversations)
	mux.HandleFunc("POST /api/conversations", s.createConversation)
	mux.HandleFunc("PATCH /api/conversations/{id}", s.updateConversation)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.deleteConversation)
	mux.HandleFunc("GET /api/conversations/{id}/messages", s.listMessages)
	mux.HandleFunc("POST /api/conversations/{id}/messages", s.createMessage)
	mux.HandleFunc("GET /", s.serveWebApp)
	return s.cors(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.statusProvider(r.Context()))
}

func (s *Server) getAgentStatus(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime != nil {
		writeJSON(w, http.StatusOK, s.agentRuntime.Status(r.Context()))
		return
	}
	if s.agentProvider == nil {
		writeJSON(w, http.StatusOK, agent.NewRuntime("").Status(r.Context()))
		return
	}
	writeJSON(w, http.StatusOK, s.agentProvider(r.Context()))
}

func (s *Server) getAgentRunSummary(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, agent.NewRuntime("").RunSummary(r.Context()))
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.RunSummary(r.Context()))
}

func (s *Server) listAgentRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListAgentRuns(r.Context())
	if err != nil {
		slog.Error("list agent runs", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not list agent runs.")
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) createAgentRun(w http.ResponseWriter, r *http.Request) {
	summary := agent.NewRuntime("").RunSummary(r.Context())
	if s.agentRuntime != nil {
		summary = s.agentRuntime.RunSummary(r.Context())
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		slog.Error("encode agent run summary", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not save agent run.")
		return
	}
	run, err := s.store.AddAgentRun(r.Context(), summary.State, payload)
	if err != nil {
		slog.Error("save agent run", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not save agent run.")
		return
	}
	s.recordAgentTrace(r.Context(), "agent run", summary.State, run.ID)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) listAgentSubagents(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.Subagent{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListSubagents(r.Context()))
}

func (s *Server) listAgentSubagentRuns(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.SubagentRun{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListSubagentRuns(r.Context()))
}

func (s *Server) runAgentSubagent(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent subagents are not available.")
		return
	}
	var input agent.SubagentRunInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	run, err := s.agentRuntime.RunSubagent(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.recordAgentTrace(r.Context(), "subagent run", run.State, run.SubagentID)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) listAgentMCPServers(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.MCPServer{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListMCPServers(r.Context()))
}

func (s *Server) listAgentMCPTools(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.MCPTool{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListMCPTools(r.Context()))
}

func (s *Server) listAgentMCPCalls(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.MCPCall{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListMCPCalls(r.Context()))
}

func (s *Server) callAgentMCPTool(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "MCP tools are not available.")
		return
	}
	var input agent.MCPCallInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	call, err := s.agentRuntime.CallMCPTool(r.Context(), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "mcp call", call.State, call.ToolID)
	writeJSON(w, http.StatusCreated, call)
}

func (s *Server) listAgentTraces(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.Trace{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListTraces(r.Context()))
}

func (s *Server) createAgentTrace(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent traces are not available.")
		return
	}
	var input agent.TraceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	trace, err := s.agentRuntime.AddTrace(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, trace)
}

func (s *Server) listAgentHookRuns(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.HookRun{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListHookRuns(r.Context()))
}

func (s *Server) createAgentHookRun(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent hook runs are not available.")
		return
	}
	var input agent.HookRunInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	run, err := s.agentRuntime.AddHookRun(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.recordAgentTrace(r.Context(), "hook run", run.State, run.HookID)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) runAgentHook(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent hooks are not available.")
		return
	}
	var input agent.HookExecutionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	execution, err := s.agentRuntime.RunHook(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "hook execution", execution.HookRun.State, execution.HookRun.HookID)
	writeJSON(w, http.StatusCreated, execution)
}

func (s *Server) listAgentSkillRuns(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.SkillRun{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListSkillRuns(r.Context()))
}

func (s *Server) runAgentSkill(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent skills are not available.")
		return
	}
	var input agent.SkillExecutionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	execution, err := s.agentRuntime.RunSkill(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "skill execution", execution.SkillRun.State, execution.SkillRun.SkillID)
	writeJSON(w, http.StatusCreated, execution)
}

func (s *Server) listAgentLoops(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.AgentLoop{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListAgentLoops(r.Context()))
}

func (s *Server) startAgentLoop(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent loops are not available.")
		return
	}
	var input agent.AgentLoopInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	loop, err := s.agentRuntime.StartAgentLoop(r.Context(), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "agent loop", loop.State, loop.Goal)
	writeJSON(w, http.StatusCreated, loop)
}

func (s *Server) continueAgentLoop(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent loops are not available.")
		return
	}
	var input agent.AgentLoopContinueInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	loop, err := s.agentRuntime.ContinueAgentLoop(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.recordAgentTrace(r.Context(), "agent loop", loop.State, loop.ID)
	writeJSON(w, http.StatusOK, loop)
}

func (s *Server) cancelAgentLoop(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent loops are not available.")
		return
	}
	loop, err := s.agentRuntime.CancelAgentLoop(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.recordAgentTrace(r.Context(), "agent loop", loop.State, loop.ID)
	writeJSON(w, http.StatusOK, loop)
}

func (s *Server) listAgentCommandApprovals(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.CommandApproval{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListCommandApprovals(r.Context()))
}

func (s *Server) createAgentCommandApproval(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent command approvals are not available.")
		return
	}
	var input agent.CommandApprovalInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	approval, err := s.agentRuntime.AddCommandApproval(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.recordAgentTrace(r.Context(), "command approval", approval.State, approval.Command)
	writeJSON(w, http.StatusCreated, approval)
}

func (s *Server) listAgentCommandChecks(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.CommandCheck{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListCommandChecks(r.Context()))
}

func (s *Server) createAgentCommandCheck(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent command checks are not available.")
		return
	}
	var input agent.CommandCheckInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	check, err := s.agentRuntime.CheckCommand(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := "blocked"
	if check.Allowed {
		state = "allowed"
	}
	s.recordAgentTrace(r.Context(), "command check", state, check.Command)
	writeJSON(w, http.StatusCreated, check)
}

func (s *Server) listAgentCommandRuns(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.CommandRun{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListCommandRuns(r.Context()))
}

func (s *Server) createAgentCommandRun(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent command runs are not available.")
		return
	}
	var input agent.CommandCheckInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	run, err := s.agentRuntime.RunCommand(r.Context(), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	state := "failed"
	if run.ExitCode == 0 {
		state = "completed"
	}
	s.recordAgentTrace(r.Context(), "command run", state, run.Command)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) readAgentWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	result, err := s.agentRuntime.ReadFile(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "read file", "completed", result.Path)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) searchAgentWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	results, err := s.agentRuntime.SearchFiles(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "search files", "completed", r.URL.Query().Get("q"))
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) updateAgentWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	var input struct {
		Root string `json:"root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	root, err := s.agentRuntime.SetWorkspaceRoot(input.Root)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	state := "disabled"
	if root != "" {
		state = "updated"
	}
	s.recordAgentTrace(r.Context(), "workspace root", state, root)
	writeJSON(w, http.StatusOK, map[string]string{"root": root})
}

func (s *Server) listAgentWorkspaceDiagnostics(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	diagnostics, err := s.agentRuntime.ListDiagnostics(r.Context())
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "read diagnostics", "completed", fmt.Sprintf("%d", len(diagnostics)))
	writeJSON(w, http.StatusOK, diagnostics)
}

func (s *Server) listAgentWorkspaceSymbols(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	symbols, err := s.agentRuntime.ListSymbols(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "read symbols", "completed", fmt.Sprintf("%d", len(symbols)))
	writeJSON(w, http.StatusOK, symbols)
}

func (s *Server) listAgentEditProposals(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.EditProposal{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListEditProposals(r.Context()))
}

func (s *Server) createAgentEditProposal(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent edit proposals are not available.")
		return
	}
	var input agent.EditProposalInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	proposal, err := s.agentRuntime.ProposeEdit(r.Context(), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "propose edit", "pending", proposal.Path)
	writeJSON(w, http.StatusCreated, proposal)
}

func (s *Server) reviewAgentEditProposal(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent edit proposals are not available.")
		return
	}
	var input agent.EditProposalReviewInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	proposal, err := s.agentRuntime.ReviewEditProposal(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "review edit", proposal.Status, proposal.Path)
	writeJSON(w, http.StatusOK, proposal)
}

func (s *Server) applyAgentEditProposal(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent edit proposals are not available.")
		return
	}
	proposal, err := s.agentRuntime.ApplyEditProposal(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "apply edit", proposal.Status, proposal.Path)
	writeJSON(w, http.StatusOK, proposal)
}

func (s *Server) recordAgentTrace(ctx context.Context, event string, state string, detail string) {
	if s.agentRuntime == nil {
		return
	}
	if _, err := s.agentRuntime.AddTrace(ctx, agent.TraceInput{Event: event, State: state, Detail: detail}); err != nil {
		slog.Warn("record agent trace", "error", err)
	}
}

func writeAgentToolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrWorkspaceDisabled):
		writeError(w, http.StatusNotFound, "Workspace tools are off.")
	case errors.Is(err, agent.ErrPathOutsideRoot):
		writeError(w, http.StatusBadRequest, "Path must stay inside the workspace.")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	if s.settingsStore == nil {
		writeJSON(w, http.StatusOK, Settings{})
		return
	}
	writeJSON(w, http.StatusOK, s.settingsStore.GetSettings())
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	if s.settingsStore == nil {
		writeError(w, http.StatusNotFound, "Settings are not available.")
		return
	}
	var next Settings
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	updated, err := s.settingsStore.UpdateSettings(next)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := s.store.ListConversations(r.Context())
	if err != nil {
		slog.Error("list conversations", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not list conversations.")
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "Untitled"
	}
	type importedMessage struct {
		role    string
		content string
	}
	importedMessages := make([]importedMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if role != "user" && role != "assistant" {
			writeError(w, http.StatusBadRequest, "Imported messages must be user or assistant messages.")
			return
		}
		if content == "" {
			continue
		}
		importedMessages = append(importedMessages, importedMessage{role: role, content: content})
	}
	conversation, err := s.store.CreateConversation(r.Context(), req.Title)
	if err != nil {
		slog.Error("create conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not create conversation.")
		return
	}
	for _, message := range importedMessages {
		if _, err := s.store.AddMessage(r.Context(), conversation.ID, message.role, message.content); err != nil {
			slog.Error("import message", "error", err)
			if deleteErr := s.store.DeleteConversation(context.WithoutCancel(r.Context()), conversation.ID); deleteErr != nil {
				slog.Error("rollback imported conversation", "error", deleteErr)
			}
			writeError(w, http.StatusInternalServerError, "Could not import messages.")
			return
		}
	}
	writeJSON(w, http.StatusCreated, conversation)
}

func (s *Server) updateConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "Title is required.")
		return
	}
	conversation, err := s.store.UpdateConversationTitle(r.Context(), r.PathValue("id"), title)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		slog.Error("update conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not update conversation.")
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteConversation(r.Context(), r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	} else if err != nil {
		slog.Error("delete conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not delete conversation.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := s.store.ListMessages(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		slog.Error("list messages", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not list messages.")
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Message must be multipart form data.")
		return
	}
	rawContent := r.FormValue("content")
	content := strings.TrimSpace(rawContent)
	if content == "" {
		writeError(w, http.StatusBadRequest, "Message content is required.")
		return
	}
	attachments, err := readAttachments(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	conversationID := r.PathValue("id")
	userMessage, err := s.store.AddMessage(r.Context(), conversationID, "user", content)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		slog.Error("add user message", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not save message.")
		return
	}

	if input, ok, parseErr := parseEditProposalChatCommand(rawContent); ok {
		s.streamEditProposalResponse(w, r, conversationID, userMessage, input, parseErr)
		return
	}

	messages, err := s.store.ListMessages(r.Context(), conversationID)
	if err != nil {
		slog.Error("load message history", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not load message history.")
		return
	}
	searchResults, err := s.searchIfNeeded(r.Context(), content)
	if err != nil {
		slog.Warn("web search failed", "error", err)
	}
	s.streamAssistantResponse(w, r, conversationID, userMessage, messages, attachments, searchResults)
}

func (s *Server) createTemporaryMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Message must be multipart form data.")
		return
	}
	rawContent := r.FormValue("content")
	content := strings.TrimSpace(rawContent)
	if content == "" {
		writeError(w, http.StatusBadRequest, "Message content is required.")
		return
	}
	attachments, err := readAttachments(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	messages, err := temporaryHistory(r.FormValue("history"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	userMessage := store.Message{
		ID:        store.NewID(),
		Role:      "user",
		Content:   content,
		CreatedAt: now,
	}
	messages = append(messages, userMessage)

	if input, ok, parseErr := parseEditProposalChatCommand(rawContent); ok {
		s.streamTemporaryEditProposalResponse(w, r, userMessage, input, parseErr)
		return
	}

	searchResults, err := s.searchIfNeeded(r.Context(), content)
	if err != nil {
		slog.Warn("web search failed", "error", err)
	}
	s.streamTemporaryAssistantResponse(w, r, userMessage, messages, attachments, searchResults)
}

func temporaryHistory(value string) ([]store.Message, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var input []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(value), &input); err != nil {
		return nil, errors.New("Invalid temporary chat history.")
	}
	messages := make([]store.Message, 0, len(input))
	baseTime := time.Now().UTC().Add(-time.Duration(len(input)) * time.Millisecond)
	for index, message := range input {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if role != "user" && role != "assistant" {
			return nil, errors.New("Temporary chat history must contain user or assistant messages.")
		}
		if content == "" {
			continue
		}
		messages = append(messages, store.Message{
			ID:        store.NewID(),
			Role:      role,
			Content:   content,
			CreatedAt: baseTime.Add(time.Duration(index) * time.Millisecond),
		})
	}
	return messages, nil
}

func (s *Server) streamEditProposalResponse(w http.ResponseWriter, r *http.Request, conversationID string, userMessage store.Message, input agent.EditProposalInput, parseErr error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusCreated)

	writeEvent(w, "user", userMessage)
	flusher.Flush()

	content := ""
	if parseErr != nil {
		content = parseErr.Error()
	} else if s.agentRuntime == nil {
		content = "Agent edit proposals are not available."
	} else {
		proposal, err := s.agentRuntime.ProposeEdit(r.Context(), input)
		if err != nil {
			content = err.Error()
		} else {
			s.recordAgentTrace(r.Context(), "propose edit", "pending", proposal.Path)
			content = fmt.Sprintf("Created an edit proposal for `%s`.", proposal.Path)
		}
	}

	assistantMessage, err := s.store.AddMessage(r.Context(), conversationID, "assistant", content)
	if err != nil {
		slog.Error("add edit proposal assistant message", "error", err)
		writeEvent(w, "error", map[string]string{"error": "Could not save assistant response."})
		flusher.Flush()
		return
	}
	writeEvent(w, "chunk", map[string]string{"content": content})
	writeEvent(w, "done", assistantMessage)
	flusher.Flush()
}

func (s *Server) streamTemporaryEditProposalResponse(w http.ResponseWriter, r *http.Request, userMessage store.Message, input agent.EditProposalInput, parseErr error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusCreated)

	writeEvent(w, "user", userMessage)
	flusher.Flush()

	content := ""
	if parseErr != nil {
		content = parseErr.Error()
	} else if s.agentRuntime == nil {
		content = "Agent edit proposals are not available."
	} else {
		proposal, err := s.agentRuntime.ProposeEdit(r.Context(), input)
		if err != nil {
			content = err.Error()
		} else {
			s.recordAgentTrace(r.Context(), "propose edit", "pending", proposal.Path)
			content = fmt.Sprintf("Created an edit proposal for `%s`.", proposal.Path)
		}
	}
	assistantMessage := store.Message{
		ID:        store.NewID(),
		Role:      "assistant",
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	writeEvent(w, "chunk", map[string]string{"content": content})
	writeEvent(w, "done", assistantMessage)
	flusher.Flush()
}

func (s *Server) streamAssistantResponse(w http.ResponseWriter, r *http.Request, conversationID string, userMessage store.Message, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusCreated)

	writeEvent(w, "user", userMessage)
	for _, result := range searchResults {
		writeEvent(w, "search", result)
	}
	flusher.Flush()

	var reply strings.Builder
	var currentProvider *llm.ProviderInfo
	generateCtx := llm.WithProviderCallback(r.Context(), func(info llm.ProviderInfo) {
		currentProvider = &info
		writeEvent(w, "provider", info)
		flusher.Flush()
	})
	err := s.llmClient.GenerateStream(generateCtx, messages, attachments, searchResults, func(chunk string) error {
		reply.WriteString(chunk)
		payload := map[string]any{"content": chunk}
		if currentProvider != nil {
			payload["provider"] = currentProvider
		}
		writeEvent(w, "chunk", payload)
		flusher.Flush()
		return nil
	})
	if errors.Is(err, llm.ErrMissingAPIKey) {
		content := "Linea is not configured with GEMINI_API_KEY yet."
		assistantMessage, saveErr := s.store.AddMessage(r.Context(), conversationID, "assistant", content)
		if saveErr != nil {
			slog.Error("add configuration assistant message", "error", saveErr)
			writeEvent(w, "error", map[string]string{"error": "Could not save assistant response."})
			flusher.Flush()
			return
		}
		writeEvent(w, "chunk", map[string]string{"content": content})
		writeEvent(w, "done", assistantMessage)
		flusher.Flush()
		return
	}
	if err != nil {
		slog.Error("generate assistant response", "error", err)
		if llm.HasImageAttachments(attachments) {
			writeEvent(w, "error", map[string]string{"error": "Image input uses Gemini, but Gemini could not respond right now."})
			flusher.Flush()
			return
		}
		writeEvent(w, "error", map[string]string{"error": "Could not generate a response."})
		flusher.Flush()
		return
	}
	assistantMessage, err := s.store.AddMessage(r.Context(), conversationID, "assistant", strings.TrimSpace(reply.String()))
	if err != nil {
		slog.Error("add assistant message", "error", err)
		writeEvent(w, "error", map[string]string{"error": "Could not save assistant response."})
		flusher.Flush()
		return
	}
	writeEvent(w, "done", messageWithProvider{Message: assistantMessage, Provider: currentProvider})
	flusher.Flush()
}

func (s *Server) streamTemporaryAssistantResponse(w http.ResponseWriter, r *http.Request, userMessage store.Message, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusCreated)

	writeEvent(w, "user", userMessage)
	for _, result := range searchResults {
		writeEvent(w, "search", result)
	}
	flusher.Flush()

	var reply strings.Builder
	var currentProvider *llm.ProviderInfo
	generateCtx := llm.WithProviderCallback(r.Context(), func(info llm.ProviderInfo) {
		currentProvider = &info
		writeEvent(w, "provider", info)
		flusher.Flush()
	})
	err := s.llmClient.GenerateStream(generateCtx, messages, attachments, searchResults, func(chunk string) error {
		reply.WriteString(chunk)
		payload := map[string]any{"content": chunk}
		if currentProvider != nil {
			payload["provider"] = currentProvider
		}
		writeEvent(w, "chunk", payload)
		flusher.Flush()
		return nil
	})
	if errors.Is(err, llm.ErrMissingAPIKey) {
		content := "Linea is not configured with GEMINI_API_KEY yet."
		assistantMessage := store.Message{
			ID:        store.NewID(),
			Role:      "assistant",
			Content:   content,
			CreatedAt: time.Now().UTC(),
		}
		writeEvent(w, "chunk", map[string]string{"content": content})
		writeEvent(w, "done", assistantMessage)
		flusher.Flush()
		return
	}
	if err != nil {
		slog.Error("generate temporary assistant response", "error", err)
		if llm.HasImageAttachments(attachments) {
			writeEvent(w, "error", map[string]string{"error": "Image input uses Gemini, but Gemini could not respond right now."})
			flusher.Flush()
			return
		}
		writeEvent(w, "error", map[string]string{"error": "Could not generate a response."})
		flusher.Flush()
		return
	}
	assistantMessage := store.Message{
		ID:        store.NewID(),
		Role:      "assistant",
		Content:   strings.TrimSpace(reply.String()),
		CreatedAt: time.Now().UTC(),
	}
	writeEvent(w, "done", messageWithProvider{Message: assistantMessage, Provider: currentProvider})
	flusher.Flush()
}

func parseEditProposalChatCommand(content string) (agent.EditProposalInput, bool, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	firstLine, body, hasBody := strings.Cut(normalized, "\n")
	firstLine = strings.TrimSpace(firstLine)
	lowerFirstLine := strings.ToLower(firstLine)
	pathText := ""
	switch {
	case strings.HasPrefix(lowerFirstLine, "propose edit "):
		pathText = strings.TrimSpace(firstLine[len("propose edit "):])
	case strings.HasPrefix(lowerFirstLine, "propose change "):
		pathText = strings.TrimSpace(firstLine[len("propose change "):])
	case strings.HasPrefix(lowerFirstLine, "create proposal "):
		pathText = strings.TrimSpace(firstLine[len("create proposal "):])
	case strings.HasPrefix(lowerFirstLine, "create edit proposal "):
		pathText = strings.TrimSpace(firstLine[len("create edit proposal "):])
	default:
		return agent.EditProposalInput{}, false, nil
	}
	if pathText == "" {
		return agent.EditProposalInput{}, true, errors.New("Use `propose edit <path>` followed by the proposed file content.")
	}
	if !hasBody || strings.TrimSpace(body) == "" {
		return agent.EditProposalInput{}, true, errors.New("Add the proposed file content after the first line.")
	}
	proposedContent := editProposalBody(body)
	return agent.EditProposalInput{
		Path:    pathText,
		Content: proposedContent,
		Summary: "Chat proposal",
	}, true, nil
}

func editProposalBody(body string) string {
	fencedBody := strings.TrimLeft(body, "\n")
	if !strings.HasPrefix(fencedBody, "```") {
		return body
	}
	_, content, hasContent := strings.Cut(fencedBody, "\n")
	if !hasContent {
		return ""
	}
	lineStart := 0
	for lineStart <= len(content) {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			if strings.TrimSpace(content[lineStart:]) == "```" {
				return content[:lineStart]
			}
			return content
		}
		lineEnd += lineStart
		if strings.TrimSpace(content[lineStart:lineEnd]) == "```" {
			return content[:lineStart]
		}
		lineStart = lineEnd + 1
	}
	return content
}

type messageWithProvider struct {
	store.Message
	Provider *llm.ProviderInfo `json:"provider,omitempty"`
}

func (s *Server) searchIfNeeded(ctx context.Context, content string) ([]llm.SearchResult, error) {
	if s.searchClient == nil || !search.ShouldSearch(content) {
		return nil, nil
	}
	results, err := s.searchClient.Search(ctx, search.QueryFromMessage(content))
	if err != nil {
		return nil, err
	}
	searchResults := make([]llm.SearchResult, 0, len(results))
	for _, result := range results {
		searchResults = append(searchResults, llm.SearchResult{
			Title:   result.Title,
			URL:     result.URL,
			Snippet: result.Snippet,
		})
	}
	return searchResults, nil
}

func readAttachments(r *http.Request) ([]llm.Attachment, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}
	files := r.MultipartForm.File["files"]
	attachments := make([]llm.Attachment, 0, len(files))
	for _, header := range files {
		mimeType := attachmentMIMEType(header.Filename, header.Header.Get("Content-Type"))
		limit := int64(512 * 1024)
		if strings.HasPrefix(mimeType, "image/") {
			if !isSupportedImageMIME(mimeType) {
				return nil, errors.New("Images must be PNG, JPEG, or WebP.")
			}
			limit = 2 * 1024 * 1024
		}
		if header.Size > limit {
			return nil, errors.New("Attached files must be 512 KB or smaller. Images must be 2 MB or smaller.")
		}
		file, err := header.Open()
		if err != nil {
			return nil, errors.New("Could not read an attached file.")
		}
		content, readErr := io.ReadAll(io.LimitReader(file, limit))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.New("Could not read an attached file.")
		}
		if strings.HasPrefix(mimeType, "image/") {
			attachments = append(attachments, llm.Attachment{Name: header.Filename, MIMEType: mimeType, Data: content})
			continue
		}
		attachments = append(attachments, llm.Attachment{Name: header.Filename, Content: string(content), MIMEType: mimeType})
	}
	return attachments, nil
}

func attachmentMIMEType(name string, contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType
	}
	if extType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); extType != "" {
		return strings.Split(extType, ";")[0]
	}
	return "text/plain"
}

func isSupportedImageMIME(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) serveWebApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	filePath := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/")), "/")
	if filePath == "" || filePath == "." {
		filePath = "index.html"
	}
	file, err := s.staticFiles.Open(filePath)
	if err == nil {
		defer file.Close()
		info, statErr := file.Stat()
		if statErr == nil && !info.IsDir() {
			http.ServeContent(w, r, filePath, info.ModTime(), file)
			return
		}
	}
	index, err := s.staticFiles.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer index.Close()
	info, err := index.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "index.html", info.ModTime(), index)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write json", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeEvent(w io.Writer, event string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte(`{"error":"Could not encode event."}`)
	}
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", payload)
}

package agent

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("LINEA_FAKE_MCP_SERVER") == "1" {
		runFakeMCPServer()
		return
	}
	os.Exit(m.Run())
}

func runFakeMCPServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		message, err := readMCPMessage(reader)
		if err != nil {
			return
		}
		id := intID(message["id"])
		method, _ := message["method"].(string)
		switch method {
		case "initialize":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
				},
			})
		case "tools/call":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "pong"}},
				},
			})
			if os.Getenv("LINEA_FAKE_MCP_STAYS_ALIVE") == "1" {
				time.Sleep(30 * time.Second)
			}
			return
		}
	}
}

func TestStatusLoadsRulesFile(t *testing.T) {
	rulesPath := filepath.Join(t.TempDir(), "AGENTS.md")
	writeTestFile(t, rulesPath, `# Linea

* Prefer simple solutions.
* Never commit secrets.
* Run relevant tests.
`)

	status := NewRuntime(rulesPath).Status(context.Background())

	if !status.Rules.Loaded || status.Rules.Source != rulesPath {
		t.Fatalf("rules = %#v", status.Rules)
	}
	if len(status.Rules.Summary) == 0 {
		t.Fatalf("summary is empty")
	}
	if len(status.Tools) == 0 || len(status.Hooks) == 0 || len(status.Boundaries) == 0 {
		t.Fatalf("status missing agent contract: %#v", status)
	}
	if len(status.Subagents) == 0 {
		t.Fatalf("status missing subagents: %#v", status)
	}
}

func TestRuntimeListsBoundedSubagents(t *testing.T) {
	subagents := NewRuntime("").ListSubagents(context.Background())

	if len(subagents) != 4 {
		t.Fatalf("subagents = %#v", subagents)
	}
	if subagents[0].ID != "review" || subagents[0].State != "planned" {
		t.Fatalf("first subagent = %#v", subagents[0])
	}
	for _, subagent := range subagents {
		if subagent.Purpose == "" || len(subagent.Tools) == 0 {
			t.Fatalf("subagent is incomplete: %#v", subagent)
		}
	}
}

func TestStatusUsesFallbackRulesWhenFileMissing(t *testing.T) {
	status := NewRuntime(filepath.Join(t.TempDir(), "missing.md")).Status(context.Background())

	if !status.Rules.Loaded || status.Rules.Source != "built-in" {
		t.Fatalf("rules = %#v", status.Rules)
	}
	if status.Mode != "local" {
		t.Fatalf("mode = %q, want local", status.Mode)
	}
}

func TestRuntimeStoresRecentTraces(t *testing.T) {
	runtime := NewRuntime("")

	trace, err := runtime.AddTrace(context.Background(), TraceInput{
		Event:  "before tool",
		State:  "recorded",
		Detail: "read-only",
	})
	if err != nil {
		t.Fatalf("AddTrace() error = %v", err)
	}
	if trace.ID == "" || trace.Event != "before tool" || trace.State != "recorded" {
		t.Fatalf("trace = %#v", trace)
	}

	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].ID != trace.ID {
		t.Fatalf("traces = %#v", traces)
	}
	status := runtime.Status(context.Background())
	if len(status.TraceEvents) != 1 || status.TraceEvents[0].ID != trace.ID {
		t.Fatalf("status traces = %#v", status.TraceEvents)
	}
}

func TestRuntimeRunSummaryCountsRecentState(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	if _, err := runtime.AddTrace(context.Background(), TraceInput{Event: "agent runtime", State: "ready"}); err != nil {
		t.Fatalf("AddTrace() error = %v", err)
	}
	if _, err := runtime.CheckCommand(context.Background(), CommandCheckInput{Command: "rm -rf ."}); err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}

	summary := runtime.RunSummary(context.Background())
	if summary.State != "attention" || summary.TraceEvents != 1 || summary.CommandChecks != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	status := runtime.Status(context.Background())
	if status.RunSummary.CommandChecks != 1 {
		t.Fatalf("status summary = %#v", status.RunSummary)
	}
}

func TestRuntimeStartsBoundedAgentLoop(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "agent loop notes\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "search agent and run tests",
		Query:   "agent",
		Command: "make test",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.ID == "" || loop.State != "waiting_approval" || len(loop.Steps) < 4 {
		t.Fatalf("loop = %#v", loop)
	}
	if loop.Steps[len(loop.Steps)-1].Kind != "command_approval" || loop.Steps[len(loop.Steps)-1].CreatedID == "" {
		t.Fatalf("last step = %#v", loop.Steps[len(loop.Steps)-1])
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || loops[0].ID != loop.ID {
		t.Fatalf("loops = %#v", loops)
	}
	status := runtime.Status(context.Background())
	if status.RunSummary.AgentLoops != 1 || len(status.AgentLoops) != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRuntimeAgentLoopInspectsSymbolsAndMCP(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\ntype App struct{}\nfunc Run() {}\n")
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{"mcpServers":{"docs":{"command":"node","tools":[{"name":"search_docs"}]}}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithMCPConfigPath(configPath))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{Goal: "find definition Run and inspect mcp"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	kinds := map[string]bool{}
	for _, step := range loop.Steps {
		kinds[step.Kind] = true
	}
	if !kinds["symbols"] || !kinds["mcp"] {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	for _, step := range loop.Steps {
		if step.Kind == "symbols" && step.Detail != `1 symbol(s) for "Run"` {
			t.Fatalf("symbol step = %#v", step)
		}
	}
}

func TestRuntimeAgentLoopStopsForWorkspaceInput(t *testing.T) {
	runtime := NewRuntime("")

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{Goal: "check diagnostics"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_input" {
		t.Fatalf("loop state = %q", loop.State)
	}
}

func TestRuntimeRejectsEmptyTrace(t *testing.T) {
	_, err := NewRuntime("").AddTrace(context.Background(), TraceInput{Event: " ", State: "ready"})
	if err == nil {
		t.Fatal("AddTrace() error = nil, want error")
	}
}

func TestRuntimeStoresHookRuns(t *testing.T) {
	runtime := NewRuntime("")

	run, err := runtime.AddHookRun(context.Background(), HookRunInput{
		HookID: "before_tool",
		State:  "completed",
		Detail: "read file",
	})
	if err != nil {
		t.Fatalf("AddHookRun() error = %v", err)
	}
	if run.ID == "" || run.HookID != "before_tool" || run.State != "completed" {
		t.Fatalf("run = %#v", run)
	}

	runs := runtime.ListHookRuns(context.Background())
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %#v", runs)
	}
	status := runtime.Status(context.Background())
	if len(status.HookRuns) != 1 || status.HookRuns[0].ID != run.ID {
		t.Fatalf("status hook runs = %#v", status.HookRuns)
	}
}

func TestRuntimeRejectsUnknownHookRun(t *testing.T) {
	_, err := NewRuntime("").AddHookRun(context.Background(), HookRunInput{HookID: "unknown", State: "completed"})
	if err == nil {
		t.Fatal("AddHookRun() error = nil, want error")
	}
}

func TestRuntimeRunsHookWithoutCommand(t *testing.T) {
	runtime := NewRuntime("")

	execution, err := runtime.RunHook(context.Background(), "before_tool", HookExecutionInput{Detail: "read file"})
	if err != nil {
		t.Fatalf("RunHook() error = %v", err)
	}
	if execution.HookRun.ID == "" || execution.HookRun.HookID != "before_tool" || execution.HookRun.State != "completed" {
		t.Fatalf("execution = %#v", execution)
	}
	if execution.CommandRun != nil {
		t.Fatalf("command run = %#v, want nil", execution.CommandRun)
	}
}

func TestRuntimeRunsHookCommand(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	execution, err := runtime.RunHook(context.Background(), "after_check", HookExecutionInput{Command: "printf ok", ApprovalID: approval.ID})
	if err != nil {
		t.Fatalf("RunHook() error = %v", err)
	}
	if execution.HookRun.State != "completed" || execution.HookRun.HookID != "after_check" {
		t.Fatalf("hook run = %#v", execution.HookRun)
	}
	if execution.CommandRun == nil || execution.CommandRun.Output != "ok" {
		t.Fatalf("command run = %#v", execution.CommandRun)
	}
}

func TestStatusLoadsSkillsFromDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "review-change.md"), "# Review change\n\nCommand: printf ok\n\nCheck a diff.")
	writeTestFile(t, filepath.Join(root, "debug test.md"), "# Debug test\n\nFix a failing test.")
	writeTestFile(t, filepath.Join(root, "notes.txt"), "ignore")
	runtime := NewRuntime("", WithSkillsDir(root))

	status := runtime.Status(context.Background())

	if len(status.Skills) != 2 {
		t.Fatalf("skills = %#v", status.Skills)
	}
	if status.Skills[0].ID != "debug_test" || status.Skills[0].Name != "Debug test" || status.Skills[0].State != "ready" {
		t.Fatalf("first skill = %#v", status.Skills[0])
	}
	if status.Skills[1].ID != "review_change" || status.Skills[1].Name != "Review change" || status.Skills[1].State != "ready" {
		t.Fatalf("second skill = %#v", status.Skills[1])
	}
	if status.Skills[1].Command != "printf ok" {
		t.Fatalf("second skill command = %q", status.Skills[1].Command)
	}
}

func TestStatusReportsEmptySkillsDirectory(t *testing.T) {
	runtime := NewRuntime("", WithSkillsDir(t.TempDir()))

	status := runtime.Status(context.Background())

	if len(status.Skills) != 1 || status.Skills[0].State != "empty" {
		t.Fatalf("skills = %#v", status.Skills)
	}
}

func TestStatusLoadsMCPServersFromConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "node",
      "args": ["server.js"],
      "env": {"TOKEN": "secret"},
      "tools": [{"name": "search_docs", "description": "Search docs"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	status := runtime.Status(context.Background())

	if len(status.MCPServers) != 1 {
		t.Fatalf("mcp servers = %#v", status.MCPServers)
	}
	server := status.MCPServers[0]
	if server.ID != "docs" || server.Name != "docs" || server.State != "ready" || server.Command != "node" {
		t.Fatalf("mcp server = %#v", server)
	}
	if len(server.Args) != 1 || server.Args[0] != "server.js" {
		t.Fatalf("mcp server args = %#v", server.Args)
	}
	if len(server.EnvKeys) != 1 || server.EnvKeys[0] != "TOKEN" {
		t.Fatalf("mcp server env keys = %#v", server.EnvKeys)
	}
	if len(status.MCPTools) != 1 {
		t.Fatalf("mcp tools = %#v", status.MCPTools)
	}
	tool := status.MCPTools[0]
	if tool.ID != "docs/search_docs" || tool.ServerID != "docs" || tool.Name != "search_docs" || tool.State != "ready" {
		t.Fatalf("mcp tool = %#v", tool)
	}
}

func TestRuntimeCallsMCPTool(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "tools": [{"name":"ping","description":"Ping"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	call, err := runtime.CallMCPTool(context.Background(), MCPCallInput{
		ToolID:    "docs/ping",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("CallMCPTool() error = %v", err)
	}
	if call.ID == "" || call.ToolID != "docs/ping" || call.State != "completed" || !strings.Contains(call.Output, "pong") {
		t.Fatalf("call = %#v", call)
	}
	calls := runtime.ListMCPCalls(context.Background())
	if len(calls) != 1 || calls[0].ID != call.ID {
		t.Fatalf("calls = %#v", calls)
	}
	status := runtime.Status(context.Background())
	if status.RunSummary.MCPCalls != 1 || len(status.MCPCalls) != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRuntimeCallsPersistentMCPTool(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1","LINEA_FAKE_MCP_STAYS_ALIVE":"1"},
      "tools": [{"name":"ping","description":"Ping"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	call, err := runtime.CallMCPTool(ctx, MCPCallInput{ToolID: "docs/ping"})
	if err != nil {
		t.Fatalf("CallMCPTool() error = %v", err)
	}
	if call.State != "completed" || !strings.Contains(call.Output, "pong") {
		t.Fatalf("call = %#v", call)
	}
}

func TestStatusReportsUnavailableMCPConfig(t *testing.T) {
	runtime := NewRuntime("", WithMCPConfigPath(filepath.Join(t.TempDir(), "missing.json")))

	status := runtime.Status(context.Background())

	if len(status.MCPServers) != 1 || status.MCPServers[0].State != "unavailable" {
		t.Fatalf("mcp servers = %#v", status.MCPServers)
	}
}

func TestRuntimeContinuesAgentLoopAfterCommandApproval(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run approved command",
		Command: "printf ok",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" {
		t.Fatalf("loop state = %q", loop.State)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "completed" {
		t.Fatalf("continued state = %q", continued.State)
	}
	if !loopHasStep(continued, "command_run", "completed") {
		t.Fatalf("continued steps = %#v", continued.Steps)
	}
	if !loopHasStep(continued, "review_result", "completed") {
		t.Fatalf("continued steps = %#v", continued.Steps)
	}
	runs := runtime.ListCommandRuns(context.Background())
	if len(runs) != 1 || runs[0].Output != "ok" {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestRuntimeAgentLoopReadsDiagnosticsAfterApprovedCheck(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"printf ok"}))
	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run tests and check diagnostics",
		Command: "printf ok",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "attention" {
		t.Fatalf("continued state = %q", continued.State)
	}
	diagnosticSteps := 0
	for _, step := range continued.Steps {
		if step.Kind == "diagnostics" {
			diagnosticSteps++
		}
	}
	if diagnosticSteps < 2 {
		t.Fatalf("continued steps = %#v", continued.Steps)
	}
	if continued.Steps[len(continued.Steps)-2].Detail != "1 diagnostic(s) after command" {
		t.Fatalf("diagnostics step = %#v", continued.Steps[len(continued.Steps)-2])
	}
	if continued.Steps[len(continued.Steps)-1].Kind != "diagnostics_review" || continued.Steps[len(continued.Steps)-1].State != "attention" {
		t.Fatalf("last step = %#v", continued.Steps[len(continued.Steps)-1])
	}
}

func TestRuntimeContinuesAgentLoopAfterNewLoopIsPrepended(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run approved command",
		Command: "printf ok",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	other, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{Goal: "check diagnostics"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.ID != loop.ID || continued.State != "completed" {
		t.Fatalf("continued = %#v", continued)
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 2 || loops[0].ID != other.ID || loops[0].State != other.State || loops[1].ID != loop.ID || loops[1].State != "completed" {
		t.Fatalf("loops = %#v", loops)
	}
}

func TestRuntimeMarksNonZeroLoopCommandExitBlocked(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"false"}))
	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run failing command",
		Command: "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "attention" || !loopHasStep(continued, "command_run", "blocked") {
		t.Fatalf("continued = %#v", continued)
	}
	runs := runtime.ListCommandRuns(context.Background())
	if len(runs) != 1 || runs[0].ExitCode == 0 {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestRuntimeCancelsAgentLoop(t *testing.T) {
	runtime := NewRuntime("")
	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{Goal: "check diagnostics"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_input" {
		t.Fatalf("loop state = %q", loop.State)
	}

	canceled, err := runtime.CancelAgentLoop(context.Background(), loop.ID)
	if err != nil {
		t.Fatalf("CancelAgentLoop() error = %v", err)
	}
	if canceled.State != "canceled" || canceled.Summary != "Canceled." {
		t.Fatalf("canceled = %#v", canceled)
	}
	if !loopHasStep(canceled, "cancel", "completed") {
		t.Fatalf("canceled steps = %#v", canceled.Steps)
	}
}

func TestRuntimeRunsBoundedSubagent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "agent notes\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	run, err := runtime.RunSubagent(context.Background(), "search", SubagentRunInput{Query: "agent"})
	if err != nil {
		t.Fatalf("RunSubagent() error = %v", err)
	}
	if run.SubagentID != "search" || run.State != "completed" || !strings.Contains(run.Summary, "Found 1") {
		t.Fatalf("run = %#v", run)
	}
	status := runtime.Status(context.Background())
	if status.RunSummary.SubagentRuns != 1 || len(status.SubagentRuns) != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRuntimeListsWorkspaceSymbols(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nvar Foo, Bar int\ntype App struct{}\nfunc Run() {}\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	symbols, err := runtime.ListSymbols(context.Background(), "run")
	if err != nil {
		t.Fatalf("ListSymbols() error = %v", err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Run" || symbols[0].Kind != "func" || symbols[0].Path != "main.go" {
		t.Fatalf("symbols = %#v", symbols)
	}
	symbols, err = runtime.ListSymbols(context.Background(), "bar")
	if err != nil {
		t.Fatalf("ListSymbols() error = %v", err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Bar" || symbols[0].Kind != "var" || symbols[0].Path != "main.go" {
		t.Fatalf("symbols = %#v", symbols)
	}
}

func TestRuntimeListsWorkspaceReferences(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\n// Run in a comment should not count.\nfunc Run() {}\nfunc main() { Run() }\nvar _ = \"Run\"\n")
	writeTestFile(t, filepath.Join(root, "notes.txt"), "Run\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	references, err := runtime.ListReferences(context.Background(), "Run")
	if err != nil {
		t.Fatalf("ListReferences() error = %v", err)
	}
	if len(references) != 2 {
		t.Fatalf("references = %#v", references)
	}
	if references[0].Path != "main.go" || references[0].Line != 4 || !strings.Contains(references[0].Text, "func Run") {
		t.Fatalf("first reference = %#v", references[0])
	}
	if references[1].Path != "main.go" || references[1].Line != 5 || !strings.Contains(references[1].Text, "Run()") {
		t.Fatalf("second reference = %#v", references[1])
	}
	if _, err := runtime.ListReferences(context.Background(), "Run()"); err == nil {
		t.Fatal("ListReferences() error = nil, want invalid identifier error")
	}
}

func TestRuntimeAgentLoopInspectsReferences(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc Run() {}\nfunc main() { Run() }\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{Goal: "find references Run"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	for _, step := range loop.Steps {
		if step.Kind == "references" && step.Detail == `2 reference(s) for "Run"` {
			return
		}
	}
	t.Fatalf("loop steps = %#v", loop.Steps)
}

func TestRuntimeRunsSkillWithoutCommand(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "review-change.md"), "# Review change\n\nCheck a diff.")
	runtime := NewRuntime("", WithSkillsDir(root))

	execution, err := runtime.RunSkill(context.Background(), "review_change", SkillExecutionInput{Detail: "read notes"})
	if err != nil {
		t.Fatalf("RunSkill() error = %v", err)
	}
	if execution.SkillRun.ID == "" || execution.SkillRun.SkillID != "review_change" || execution.SkillRun.State != "completed" {
		t.Fatalf("skill run = %#v", execution.SkillRun)
	}
	if execution.CommandRun != nil {
		t.Fatalf("command run = %#v, want nil", execution.CommandRun)
	}
	runs := runtime.ListSkillRuns(context.Background())
	if len(runs) != 1 || runs[0].ID != execution.SkillRun.ID {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestRuntimeRunsSkillCommand(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "review-change.md"), "# Review change\n\nCommand: printf ok\n")
	runtime := NewRuntime("", WithSkillsDir(root), WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	execution, err := runtime.RunSkill(context.Background(), "review_change", SkillExecutionInput{ApprovalID: approval.ID})
	if err != nil {
		t.Fatalf("RunSkill() error = %v", err)
	}
	if execution.SkillRun.State != "completed" || execution.SkillRun.SkillID != "review_change" {
		t.Fatalf("skill run = %#v", execution.SkillRun)
	}
	if execution.CommandRun == nil || execution.CommandRun.Output != "ok" {
		t.Fatalf("command run = %#v", execution.CommandRun)
	}
}

func TestRuntimeRejectsUnknownSkill(t *testing.T) {
	_, err := NewRuntime("").RunSkill(context.Background(), "missing", SkillExecutionInput{})
	if err == nil {
		t.Fatal("RunSkill() error = nil, want error")
	}
}

func TestRuntimeRejectsPlannedSkillExecution(t *testing.T) {
	_, err := NewRuntime("").RunSkill(context.Background(), "review_change", SkillExecutionInput{})
	if err == nil {
		t.Fatal("RunSkill() error = nil, want error")
	}
}

func TestRuntimeChecksCommandsAgainstAllowlist(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test", "go test ./..."}))

	allowed, err := runtime.CheckCommand(context.Background(), CommandCheckInput{Command: " make   test "})
	if err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	if !allowed.Allowed || allowed.Command != "make test" || allowed.Reason != "allowed" {
		t.Fatalf("allowed = %#v", allowed)
	}

	blocked, err := runtime.CheckCommand(context.Background(), CommandCheckInput{Command: "rm -rf ."})
	if err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	if blocked.Allowed || blocked.Reason != "not in allowlist" {
		t.Fatalf("blocked = %#v", blocked)
	}

	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) != 2 || checks[0].ID != blocked.ID || checks[1].ID != allowed.ID {
		t.Fatalf("checks = %#v", checks)
	}
	status := runtime.Status(context.Background())
	if len(status.CommandChecks) != 2 {
		t.Fatalf("status command checks = %#v", status.CommandChecks)
	}
}

func TestRuntimeStoresCommandApprovals(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))

	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{
		Command: " make   test ",
		State:   "approved",
		Detail:  "before commit",
	})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	if approval.ID == "" || approval.Command != "make test" || approval.State != "approved" {
		t.Fatalf("approval = %#v", approval)
	}
	approvals := runtime.ListCommandApprovals(context.Background())
	if len(approvals) != 1 || approvals[0].ID != approval.ID {
		t.Fatalf("approvals = %#v", approvals)
	}
	status := runtime.Status(context.Background())
	if len(status.CommandApprovals) != 1 || status.CommandApprovals[0].ID != approval.ID {
		t.Fatalf("status command approvals = %#v", status.CommandApprovals)
	}
}

func TestRuntimeRejectsApprovingCommandOutsideAllowlist(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))

	_, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "rm -rf .", State: "approved"})
	if err == nil {
		t.Fatal("AddCommandApproval() error = nil, want error")
	}
}

func TestRuntimeRunsCommandWithApproval(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	run, err := runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok", ApprovalID: approval.ID})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if run.Output != "ok" {
		t.Fatalf("run = %#v", run)
	}
	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) != 1 || checks[0].ApprovalID != approval.ID || !checks[0].Allowed {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeRejectsUnapprovedCommandApproval(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "pending"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	_, err = runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok", ApprovalID: approval.ID})
	if err == nil {
		t.Fatal("RunCommand() error = nil, want error")
	}
}

func TestRuntimeRejectsEmptyCommandCheck(t *testing.T) {
	_, err := NewRuntime("").CheckCommand(context.Background(), CommandCheckInput{Command: " "})
	if err == nil {
		t.Fatal("CheckCommand() error = nil, want error")
	}
}

func TestRuntimeRunsAllowedCommandInWorkspace(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	run, err := runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok", ApprovalID: approval.ID})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if run.ID == "" || run.Command != "printf ok" || run.ExitCode != 0 || run.Output != "ok" {
		t.Fatalf("run = %#v", run)
	}
	runs := runtime.ListCommandRuns(context.Background())
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %#v", runs)
	}
	status := runtime.Status(context.Background())
	if len(status.CommandRuns) != 1 || status.CommandRuns[0].ID != run.ID {
		t.Fatalf("status command runs = %#v", status.CommandRuns)
	}
}

func TestRuntimeRejectsCommandRunWithoutApproval(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))

	_, err := runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok"})
	if err == nil {
		t.Fatal("RunCommand() error = nil, want approval error")
	}
}

func TestRuntimeRejectsCommandRunOutsideAllowlist(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf no", State: "pending"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	_, err = runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf no", ApprovalID: approval.ID})
	if err == nil {
		t.Fatal("RunCommand() error = nil, want error")
	}
}

func TestRuntimeRequiresWorkspaceForCommandRun(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	_, err = runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok", ApprovalID: approval.ID})
	if !errors.Is(err, ErrWorkspaceDisabled) {
		t.Fatalf("RunCommand() error = %v, want ErrWorkspaceDisabled", err)
	}
}

func TestWorkspaceReadsFilesInsideRoot(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "Linea reads local files.")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	result, err := runtime.ReadFile(context.Background(), "notes.md")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if result.Path != "notes.md" || result.Content != "Linea reads local files." {
		t.Fatalf("result = %#v", result)
	}
}

func TestWorkspaceRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	if _, err := runtime.ReadFile(context.Background(), "../secret.txt"); !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("ReadFile() error = %v, want ErrPathOutsideRoot", err)
	}
}

func TestWorkspaceRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeTestFile(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	if _, err := runtime.ReadFile(context.Background(), "link.txt"); !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("ReadFile() error = %v, want ErrPathOutsideRoot", err)
	}
}

func TestWorkspaceSearchesTextFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.txt"), "hello\nagent trace\n")
	writeTestFile(t, filepath.Join(root, "b.txt"), "nothing\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	results, err := runtime.SearchFiles(context.Background(), "agent")
	if err != nil {
		t.Fatalf("SearchFiles() error = %v", err)
	}
	if len(results) != 1 || results[0].Path != "a.txt" || results[0].Line != 2 {
		t.Fatalf("results = %#v", results)
	}
}

func TestWorkspaceListsGoDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "ok.go"), "package main\n")
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc main( {\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	diagnostics, err := runtime.ListDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("ListDiagnostics() error = %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("ListDiagnostics() returned no diagnostics")
	}
	if diagnostics[0].Path != "broken.go" || diagnostics[0].Severity != "error" || diagnostics[0].Line == 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestWorkspaceDisabledByDefault(t *testing.T) {
	runtime := NewRuntime("")
	if runtime.WorkspaceEnabled() {
		t.Fatal("WorkspaceEnabled() = true, want false")
	}
	if _, err := runtime.SearchFiles(context.Background(), "agent"); !errors.Is(err, ErrWorkspaceDisabled) {
		t.Fatalf("SearchFiles() error = %v, want ErrWorkspaceDisabled", err)
	}
	if _, err := runtime.ListDiagnostics(context.Background()); !errors.Is(err, ErrWorkspaceDisabled) {
		t.Fatalf("ListDiagnostics() error = %v, want ErrWorkspaceDisabled", err)
	}
}

func TestProposeEditStoresPendingDiff(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "one\ntwo\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "one\nthree\n",
		Summary: "change second line",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if proposal.ID == "" || proposal.Status != "pending" || proposal.Path != "notes.md" {
		t.Fatalf("proposal = %#v", proposal)
	}
	if len(proposal.Diff) == 0 {
		t.Fatalf("diff is empty")
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].ID != proposal.ID {
		t.Fatalf("proposals = %#v", proposals)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "one\ntwo\n" {
		t.Fatalf("file was changed: %q", string(content))
	}
}

func TestProposeEditPreservesTrailingNewlineDiff(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "one")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "one\n",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if !hasAgentDiffLine(proposal.Diff, "add", "") {
		t.Fatalf("diff did not show added trailing newline: %#v", proposal.Diff)
	}

	proposal, err = runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "one",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if hasAgentDiffLine(proposal.Diff, "add", "") {
		t.Fatalf("diff added unexpected blank line: %#v", proposal.Diff)
	}
}

func hasAgentDiffLine(lines []DiffLine, lineType string, text string) bool {
	for _, line := range lines {
		if line.Type == lineType && line.Text == text {
			return true
		}
	}
	return false
}

func TestReviewEditProposalUpdatesStatusWithoutWriting(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "one\ntwo\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))
	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "one\nthree\n",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}

	reviewed, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, EditProposalReviewInput{
		Status: "approved",
		Detail: "looks right",
	})
	if err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	if reviewed.Status != "approved" || reviewed.ReviewDetail != "looks right" || reviewed.ReviewedAt == nil {
		t.Fatalf("reviewed = %#v", reviewed)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Status != "approved" {
		t.Fatalf("proposals = %#v", proposals)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "one\ntwo\n" {
		t.Fatalf("file was changed: %q", string(content))
	}
}

func TestApplyEditProposalRequiresApproval(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "one\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))
	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "two\n",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}

	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err == nil {
		t.Fatal("ApplyEditProposal() error = nil, want error")
	}
	content, err := os.ReadFile(filepath.Join(root, "notes.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "one\n" {
		t.Fatalf("file was changed: %q", string(content))
	}
}

func TestApplyEditProposalWritesApprovedContent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "one\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))
	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "two\n",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}

	applied, err := runtime.ApplyEditProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("ApplyEditProposal() error = %v", err)
	}
	if applied.Status != "applied" || applied.AppliedAt == nil {
		t.Fatalf("applied = %#v", applied)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "two\n" {
		t.Fatalf("file content = %q, want two", string(content))
	}
}

func TestApplyEditProposalRejectsStaleFile(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "notes.md")
	writeTestFile(t, filePath, "one\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))
	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "notes.md",
		Content: "two\n",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	writeTestFile(t, filePath, "local change\n")

	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err == nil {
		t.Fatal("ApplyEditProposal() error = nil, want stale error")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "local change\n" {
		t.Fatalf("file content = %q, want local change", string(content))
	}
}

func TestSetWorkspaceRootClearsProposals(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeTestFile(t, filepath.Join(firstRoot, "notes.md"), "one\n")
	runtime := NewRuntime("", WithWorkspaceRoot(firstRoot))
	if _, err := runtime.ProposeEdit(context.Background(), EditProposalInput{Path: "notes.md", Content: "two\n"}); err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}

	root, err := runtime.SetWorkspaceRoot(secondRoot)
	if err != nil {
		t.Fatalf("SetWorkspaceRoot() error = %v", err)
	}
	if root == "" || runtime.WorkspaceRoot() != root {
		t.Fatalf("workspace root = %q, want %q", runtime.WorkspaceRoot(), root)
	}
	if proposals := runtime.ListEditProposals(context.Background()); len(proposals) != 0 {
		t.Fatalf("proposals = %#v, want empty", proposals)
	}
}

func TestReviewEditProposalRejectsInvalidStatus(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))

	_, err := runtime.ReviewEditProposal(context.Background(), "proposal", EditProposalReviewInput{Status: "applied"})
	if err == nil {
		t.Fatal("ReviewEditProposal() error = nil, want error")
	}
}

func TestProposeEditRejectsOutsideRoot(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))

	_, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "../notes.md",
		Content: "x",
	})
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("ProposeEdit() error = %v, want ErrPathOutsideRoot", err)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func loopHasStep(loop AgentLoop, kind string, state string) bool {
	for _, step := range loop.Steps {
		if step.Kind == kind && step.State == state {
			return true
		}
	}
	return false
}

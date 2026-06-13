package agent

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("LINEA_FAKE_MCP_SERVER") == "1" {
		runFakeMCPServer()
		return
	}
	f, _ := os.CreateTemp("", "linea-audit-*.jsonl")
	if f != nil {
		os.Setenv("LINEA_AUDIT_LOG_PATH", f.Name())
		f.Close()
	}
	os.Unsetenv("LINEA_AUTO_APPROVE_CATEGORIES")
	code := m.Run()
	if f != nil {
		os.Remove(f.Name())
	}
	os.Exit(code)
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
		case "tools/list":
			params, _ := message["params"].(map[string]any)
			cursor, _ := params["cursor"].(string)
			if os.Getenv("LINEA_FAKE_MCP_PAGED_TOOLS") == "1" && cursor == "" {
				_ = writeMCPMessage(os.Stdout, map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"nextCursor": "next",
						"tools": []map[string]any{{
							"name":        "ping",
							"description": "Ping",
							"inputSchema": map[string]any{"type": "object"},
						}},
					},
				})
				continue
			}
			if os.Getenv("LINEA_FAKE_MCP_PAGED_TOOLS") == "1" {
				_ = writeMCPMessage(os.Stdout, map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"tools": []map[string]any{{
							"name":        "lookup",
							"description": "Lookup",
							"inputSchema": map[string]any{"type": "object"},
						}},
					},
				})
				continue
			}
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "ping",
						"description": "Ping",
						"inputSchema": map[string]any{"type": "object"},
					}},
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
		case "resources/list":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"resources": []map[string]any{{
						"uri":         "docs://readme",
						"name":        "README",
						"description": "Project README",
						"mimeType":    "text/markdown",
					}},
				},
			})
		case "resources/read":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"contents": []map[string]any{{"uri": "docs://readme", "mimeType": "text/markdown", "text": "# README"}},
				},
			})
			return
		case "resources/subscribe":
			params, _ := message["params"].(map[string]any)
			uri, _ := params["uri"].(string)
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{},
			})
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"method":  "notifications/resources/updated",
				"params":  map[string]any{"uri": uri, "message": "resource updated"},
			})
		case "resources/unsubscribe":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{},
			})
			return
		case "prompts/list":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"prompts": []map[string]any{{"name": "review", "description": "Review code"}},
				},
			})
		case "prompts/get":
			_ = writeMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"messages": []map[string]any{{"role": "user", "content": map[string]any{"type": "text", "text": "Review this"}}},
				},
			})
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
	if subagents[0].ID != "review" || subagents[0].State != "ready" {
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
	if loop.Mode != "guided" || loop.MaxIterations != 0 {
		t.Fatalf("loop mode = %q, max = %d", loop.Mode, loop.MaxIterations)
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

func TestRuntimeStartsAutoAgentLoop(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "run approved command",
		Mode:          "auto",
		MaxIterations: 50,
		Command:       "printf ok",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.Mode != "auto" || loop.MaxIterations != maxAutoLoopIterationsLimit {
		t.Fatalf("loop mode = %q, max = %d", loop.Mode, loop.MaxIterations)
	}
	if loop.State != "completed" || !loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	if loop.Steps[len(loop.Steps)-1].Detail != "Command completed successfully." {
		t.Fatalf("last step = %#v", loop.Steps[len(loop.Steps)-1])
	}
}

func TestRuntimeDeveloperAgentLoopRunsNonDestructiveCommand(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "run command",
		Mode:          "developer",
		MaxIterations: 2,
		Command:       "printf ok",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.Mode != "developer" || loop.MaxIterations != 2 {
		t.Fatalf("loop mode = %q, max = %d", loop.Mode, loop.MaxIterations)
	}
	if loop.State != "completed" || !loopHasStep(loop, "command_check", "completed") || !loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) == 0 || !checks[0].Allowed || checks[0].Reason != "developer mode" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeDeveloperAgentLoopInspectsFailedCommand(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "README.md"), "notes")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || !loopHasStep(loop, "command_followup", "completed") || !loopHasStep(loop, "retry", "waiting_input") {
		t.Fatalf("loop = %#v", loop)
	}
	if !loopStepHasCommand(loop, "command_followup", "find . -maxdepth 2 -type f") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	if !loopStepHasCommand(loop, "command_run", "find . -maxdepth 2 -type f") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
}

func TestRuntimeDeveloperAgentLoopBlocksDestructiveCommand(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "rm -rf .",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || !loopHasStep(loop, "command_check", "blocked") || loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) == 0 || checks[0].Allowed || checks[0].Reason != "developer mode blocked destructive or system command" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeDeveloperAgentLoopBlocksShellWrappedCommand(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "sh -c rm -rf .",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || !loopHasStep(loop, "command_check", "blocked") || loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) == 0 || checks[0].Allowed || checks[0].Reason != "developer mode blocked destructive or system command" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeDeveloperAgentLoopBlocksCredentialReadCommand(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "read env",
		Mode:    "developer",
		Command: "cat .env",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || !loopHasStep(loop, "command_check", "blocked") || loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeDeveloperAgentLoopBlocksBroadSystemCommands(t *testing.T) {
	for _, command := range []string{
		"brew install example",
		"launchctl list",
		"systemctl restart postgres",
		"git push origin main",
		"npm install -g typescript",
		"pip install --user pytest",
	} {
		t.Run(command, func(t *testing.T) {
			runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
			loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
				Goal:    "run command",
				Mode:    "developer",
				Command: command,
			})
			if err != nil {
				t.Fatalf("StartAgentLoop() error = %v", err)
			}
			if loop.State != "attention" || !loopHasStep(loop, "command_check", "blocked") || loopHasStep(loop, "command_run", "completed") {
				t.Fatalf("loop = %#v", loop)
			}
			checks := runtime.ListCommandChecks(context.Background())
			if len(checks) == 0 || checks[0].Allowed || checks[0].Reason != "developer mode blocked destructive or system command" {
				t.Fatalf("checks = %#v", checks)
			}
		})
	}
}

func TestRuntimeDeveloperAgentLoopAllowsProjectScopedPackageCommands(t *testing.T) {
	runtime := NewRuntime("")
	for _, command := range []string{
		"npm install",
		"npm run build",
		"go mod download",
		"git status --short",
	} {
		t.Run(command, func(t *testing.T) {
			reason, err := checkDeveloperCommand(runtime, command)
			if err != nil {
				t.Fatalf("checkDeveloperCommand(%q) error = %v", command, err)
			}
			if reason != "developer mode" {
				t.Fatalf("reason = %q", reason)
			}
		})
	}
}

func TestRuntimeDeveloperAgentLoopInfersInstallCommand(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"scripts":{}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "install dependencies",
		Mode: "developer",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopHasStep(loop, "command_infer", "completed") || !loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	if !loopStepHasCommand(loop, "command_run", "npm install") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
}

func TestRuntimeCommandOutputRedactsSecretsInTimeline(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "printf"), "#!/bin/sh\nprintf 'API_KEY=secret-value\\n'\n")
	if err := os.Chmod(filepath.Join(root, "printf"), 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "./printf",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	runs := runtime.ListCommandRuns(context.Background())
	if len(runs) == 0 || strings.Contains(runs[0].Output, "secret-value") || !strings.Contains(runs[0].Output, "[redacted]") {
		t.Fatalf("runs = %#v", runs)
	}
	for _, step := range loop.Steps {
		if strings.Contains(step.Detail, "secret-value") {
			t.Fatalf("step leaked secret = %#v", step)
		}
	}
}

func TestRuntimeAutoAgentLoopPlansEditFromDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix diagnostics",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "plan_edit", "completed") || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if !loopHasStep(loop, "subagent_plan", "completed") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "broken.go" || proposals[0].Content != "package main\nfunc fixed() {}\n" {
		t.Fatalf("proposals = %#v", proposals)
	}
	if proposals[0].Status != "pending" {
		t.Fatalf("proposal status = %q", proposals[0].Status)
	}
	if len(planner.requests) != 1 || len(planner.requests[0].Diagnostics) != 1 || len(planner.requests[0].Files) != 1 {
		t.Fatalf("planner requests = %#v", planner.requests)
	}
	if status := runtime.Status(context.Background()); status.RunSummary.SubagentRuns != 1 || len(status.SubagentPlans) != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRuntimeAutoAgentLoopGathersEvidenceForBroadFix(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix the project",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "diagnostics", "completed") || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if !loopHasStep(loop, "subagent_plan", "completed") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	if len(planner.requests) != 1 || len(planner.requests[0].Diagnostics) != 1 || len(planner.requests[0].Files) != 1 {
		t.Fatalf("planner requests = %#v", planner.requests)
	}
	if plans := runtime.ListSubagentPlans(context.Background()); len(plans) != 1 || plans[0].SubagentIDs[0] != "review" {
		t.Fatalf("subagent plans = %#v", plans)
	}
}

func TestRuntimeAutoAgentLoopUsesSearchSubagent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "agent notes\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "search agent",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopHasStep(loop, "search_files", "completed") || !loopHasStep(loop, "subagent_plan", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	runs := runtime.ListSubagentRuns(context.Background())
	if len(runs) != 1 || runs[0].SubagentID != "search" || !strings.Contains(runs[0].Summary, "Found 1") {
		t.Fatalf("subagent runs = %#v", runs)
	}
	plans := runtime.ListSubagentPlans(context.Background())
	if len(plans) != 1 || len(plans[0].Runs) != 1 || plans[0].SubagentIDs[0] != "search" {
		t.Fatalf("subagent plans = %#v", plans)
	}
}

func TestRuntimeAutoAgentLoopRunsInferredProjectCheckCommand(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix and test the project",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || !loopHasStep(loop, "command_infer", "completed") || !loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	approvals := runtime.ListCommandApprovals(context.Background())
	if len(approvals) != 1 || approvals[0].Command != "make test" || approvals[0].State != "approved" {
		t.Fatalf("approvals = %#v", approvals)
	}
	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) != 1 || checks[0].Command != "make test" || !checks[0].Allowed || checks[0].Reason != "inferred project check" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeAutoAgentLoopRunsInferredPackageBuildCommand(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"scripts":{"build":"printf ok","test":"vitest"}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix the React frontend build",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || !loopHasStep(loop, "command_infer", "completed") || !loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoAgentLoopStillBlocksExplicitUnallowedCommand(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "auto",
		Command: "printf ok",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" {
		t.Fatalf("loop = %#v", loop)
	}
	if loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	checks := runtime.ListCommandChecks(context.Background())
	if len(checks) != 1 || checks[0].Allowed {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeAutoAgentLoopWaitsForEditBeforeBuild(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "create a small React portfolio, propose the needed file changes first, run the build check after approval, and report how to preview it",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "edit_boundary", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if loopHasStep(loop, "command_approval", "waiting_approval") || loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop should wait for edit input before command work: %#v", loop)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if len(continued.Steps) != len(loop.Steps) || continued.State != "waiting_approval" {
		t.Fatalf("continued = %#v", continued)
	}
}

func TestRuntimeAutoAgentLoopPlansCreateProposal(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	writeTestFile(t, filepath.Join(root, "portfolio.html"), "existing portfolio\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "portfolio.html",
			Content: "<!doctype html>\n<div id=\"root\"></div>\n",
			Summary: "Create portfolio",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "create a small React portfolio, propose the needed file changes first, run the build check after approval, and report how to preview it",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "plan_edit", "completed") || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if loopHasStep(loop, "command_approval", "waiting_approval") || loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop should wait for proposal review before command work: %#v", loop)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "portfolio.html" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
	if len(planner.requests) != 1 || len(planner.requests[0].Files) != 1 || planner.requests[0].Files[0].Path != "portfolio.html" {
		t.Fatalf("planner requests = %#v", planner.requests)
	}
	if planner.requests[0].Files[0].Content != "existing portfolio\n" {
		t.Fatalf("planner file content = %q", planner.requests[0].Files[0].Content)
	}
}

func TestRuntimeAutoAgentLoopStopsAfterCreatingProposalAtLimit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "portfolio.html",
			Content: "<!doctype html>\n<div id=\"root\"></div>\n",
			Summary: "Create portfolio",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "create a small React portfolio and run tests",
		Mode:          "auto",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if loopHasStep(loop, "command_run", "completed") || loopHasStep(loop, "command_infer", "completed") {
		t.Fatalf("loop ran command work after edit limit: %#v", loop.Steps)
	}
}

func TestRuntimeAutoAgentLoopUsesCreateFallbackWhenPlannerFails(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{err: errors.New("bad planner json")}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "create a React portfolio",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "portfolio.html" || proposals[0].Status != "pending" || !strings.Contains(proposals[0].Content, "Product engineer portfolio") {
		t.Fatalf("proposals = %#v", proposals)
	}
}

func TestRuntimeAutoAgentLoopDoesNotFallbackOverExistingCreateTarget(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "portfolio.html"), "existing portfolio\n")
	planner := &fakeEditPlanner{err: errors.New("bad planner json")}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "create a React portfolio",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || !loopHasStep(loop, "plan_edit", "blocked") {
		t.Fatalf("loop = %#v", loop)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 0 {
		t.Fatalf("proposals = %#v", proposals)
	}
	data, err := os.ReadFile(filepath.Join(root, "portfolio.html"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "existing portfolio\n" {
		t.Fatalf("file content = %q", data)
	}
}

func TestRuntimeAutoAgentLoopContinuesFromAppliedProposalToInferredCheck(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix diagnostics and run tests",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 {
		t.Fatalf("proposals = %#v", proposals)
	}
	if proposals[0].Status != "pending" {
		t.Fatalf("proposal status = %q", proposals[0].Status)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposals[0].ID, EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	if _, err := runtime.ApplyEditProposal(context.Background(), proposals[0].ID); err != nil {
		t.Fatalf("ApplyEditProposal() error = %v", err)
	}
	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "completed" || !loopHasStep(continued, "edit_review", "completed") || !loopHasStep(continued, "command_run", "completed") {
		t.Fatalf("continued = %#v", continued)
	}
}

func TestRuntimeAutoAgentLoopCanApplyGeneratedProposal(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "fix diagnostics and run tests",
		Mode:      "auto",
		AutoApply: true,
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || !loop.AutoApply || !loopHasStep(loop, "edit_review", "completed") || !loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	content, err := os.ReadFile(filepath.Join(root, "broken.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package main\nfunc fixed() {}\n" {
		t.Fatalf("content = %q", content)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Status != "applied" {
		t.Fatalf("proposals = %#v", proposals)
	}
}

func TestRuntimeAutoAgentLoopProposesEditAfterFailedCheckDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc ok() {}\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"false"}), WithEditPlanner(planner))
	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "fix tests",
		Mode:    "auto",
		Command: "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "waiting_approval" || !loopHasStep(continued, "command_run", "blocked") || !loopHasStep(continued, "edit_review", "waiting_approval") {
		t.Fatalf("continued = %#v", continued)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "broken.go" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
	if len(planner.requests) != 1 || len(planner.requests[0].Diagnostics) != 1 {
		t.Fatalf("planner requests = %#v", planner.requests)
	}
}

func TestRuntimeAutoAgentLoopRerunsCommandAfterAutoAppliedFix(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc ok() {}\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"false"}), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "fix tests",
		Mode:      "auto",
		AutoApply: true,
		Command:   "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{AutoApply: true})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "attention" || !loopHasStep(continued, "edit_review", "completed") || !loopHasStep(continued, "command_retry", "completed") {
		t.Fatalf("continued = %#v", continued)
	}
	count := 0
	for _, step := range continued.Steps {
		if step.Kind == "command_run" && step.Command == "false" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("command run count = %d, steps = %#v", count, continued.Steps)
	}
}

func TestRuntimeAutoAgentLoopRepeatsAutoFixUntilCommandPasses(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc ok() {}\n")
	writeTestFile(t, filepath.Join(root, "check"), `#!/bin/sh
count=0
if [ -f .check-count ]; then
  count=$(cat .check-count)
fi
count=$((count + 1))
printf "%s" "$count" > .check-count
if [ "$count" -lt 3 ]; then
  printf "package main\nfunc broken( {\n" > broken.go
  exit 1
fi
exit 0
`)
	if err := os.Chmod(filepath.Join(root, "check"), 0o700); err != nil {
		t.Fatal(err)
	}
	planner := &fakeEditPlanner{
		plans: []EditPlan{
			{Path: "broken.go", Content: "package main\nfunc fixedOnce() {}\n", Summary: "First fix"},
			{Path: "broken.go", Content: "package main\nfunc fixedTwice() {}\n", Summary: "Second fix"},
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"./check"}), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "fix tests",
		Mode:      "auto",
		AutoApply: true,
		Command:   "./check",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" {
		t.Fatalf("loop = %#v", loop)
	}
	commandRuns := 0
	editProposals := 0
	for _, step := range loop.Steps {
		if step.Kind == "command_run" && step.Command == "./check" {
			commandRuns++
		}
		if step.Kind == "edit_proposal" {
			editProposals++
		}
	}
	if commandRuns != 3 || editProposals != 2 {
		t.Fatalf("command runs = %d, edit proposals = %d, steps = %#v", commandRuns, editProposals, loop.Steps)
	}
	if len(planner.requests) != 2 {
		t.Fatalf("planner requests = %#v", planner.requests)
	}
}

func TestRuntimeAutoAgentLoopAllowsPlannerPathOutsideDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	writeTestFile(t, filepath.Join(root, "other.go"), "package main\nfunc other() {}\n")
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "other.go",
			Content: "package main\nfunc changed() {}\n",
			Summary: "Wrong file",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix diagnostics",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	if proposals := runtime.ListEditProposals(context.Background()); len(proposals) != 1 || proposals[0].Path != "other.go" {
		t.Fatalf("proposals = %#v, want 1 for other.go", proposals)
	}
}

func TestRuntimeAutoAgentLoopHonorsContinueMaxIterations(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "old\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"false"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "fix notes",
		Mode:          "auto",
		MaxIterations: 1,
		Command:       "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	limited, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if limited.State != "waiting_input" || limited.MaxIterations != 1 || !loopHasStep(limited, "command_run", "blocked") {
		t.Fatalf("limited = %#v", limited)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{
		MaxIterations:   2,
		ProposalPath:    "notes.md",
		ProposalContent: "new\n",
	})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.MaxIterations != 2 {
		t.Fatalf("continued max iterations = %d, want 2", continued.MaxIterations)
	}
	if continued.State != "waiting_approval" || !loopHasStep(continued, "edit_proposal", "completed") || !loopHasStep(continued, "edit_review", "waiting_approval") {
		t.Fatalf("continued = %#v", continued)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "notes.md" {
		t.Fatalf("proposals = %#v", proposals)
	}
}

func TestRuntimeAutoAgentLoopAdvancesLimitForExplicitContinueInput(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "old\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"false"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "fix notes",
		Mode:          "auto",
		MaxIterations: 1,
		Command:       "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	limited, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if limited.State != "waiting_input" {
		t.Fatalf("loop = %#v", loop)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{
		ProposalPath:    "notes.md",
		ProposalContent: "new\n",
	})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.MaxIterations != 2 {
		t.Fatalf("continued max iterations = %d, want 2", continued.MaxIterations)
	}
	if continued.State != "waiting_approval" || !loopHasStep(continued, "edit_proposal", "completed") || !loopHasStep(continued, "edit_review", "waiting_approval") {
		t.Fatalf("continued = %#v", continued)
	}
}

func TestRuntimeAutoAgentLoopKeepsLimitForEmptyContinue(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "old\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"false"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "fix notes",
		Mode:          "auto",
		MaxIterations: 1,
		Command:       "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	if _, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{}); err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.MaxIterations != 1 || continued.State != "waiting_input" || !loopHasStep(continued, "auto_limit", "waiting_input") {
		t.Fatalf("continued = %#v", continued)
	}
}

func TestRuntimeAutoAgentLoopStopsBeforeCommandAfterProposalReachesLimit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "old\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"false"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "fix notes",
		Mode:          "auto",
		MaxIterations: 1,
		Command:       "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "false", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	if _, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{}); err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{
		MaxIterations:   2,
		ProposalPath:    "notes.md",
		ProposalContent: "new\n",
		Command:         "false",
	})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	if continued.State != "waiting_approval" || !loopHasStep(continued, "edit_proposal", "completed") || !loopHasStep(continued, "edit_review", "waiting_approval") {
		t.Fatalf("continued = %#v", continued)
	}
	if loopHasStep(continued, "command_approval", "waiting_approval") {
		t.Fatalf("continued queued command approval after limit: %#v", continued.Steps)
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

func TestRuntimeAgentLoopPlansNamedMCPToolWithoutCalling(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "tools": [{"name":"ping","description":"Ping","inputSchema":"{\"type\":\"object\"}"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{Goal: "use mcp tool ping"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopHasStep(loop, "mcp_plan", "completed") || !loopHasStep(loop, "mcp_boundary", "completed") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	if loopHasStep(loop, "mcp_call", "completed") {
		t.Fatalf("loop called MCP tool before approval: %#v", loop.Steps)
	}
	calls := runtime.ListMCPCalls(context.Background())
	if len(calls) != 0 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRuntimeAutoAgentLoopCallsEmptyArgumentMCPTool(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "tools": [{"name":"ping","description":"Ping","inputSchema":"{\"type\":\"object\"}"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "use mcp tool ping",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || !loopHasStep(loop, "mcp_call", "completed") || !loopHasStep(loop, "mcp_validate", "completed") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	if loopHasStep(loop, "mcp_boundary", "completed") {
		t.Fatalf("loop stopped at MCP boundary: %#v", loop.Steps)
	}
	calls := runtime.ListMCPCalls(context.Background())
	if len(calls) != 1 || calls[0].ToolID != "docs/ping" || calls[0].State != "completed" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRuntimeMCPResourceSubscriptionRecordsEvents(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "resources": [{"uri":"docs://readme","name":"README","description":"Readme"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	subscription, err := runtime.SubscribeMCPResource(context.Background(), MCPSubscribeInput{URI: "docs://readme"})
	if err != nil {
		t.Fatalf("SubscribeMCPResource() error = %v", err)
	}
	if subscription.State != "active" || subscription.URI != "docs://readme" {
		t.Fatalf("subscription = %#v", subscription)
	}
	var events []MCPEvent
	for attempt := 0; attempt < 20; attempt++ {
		events = runtime.ListMCPEvents(context.Background())
		if len(events) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) == 0 || events[0].SubscriptionID != subscription.ID || events[0].Method != "notifications/resources/updated" {
		t.Fatalf("events = %#v", events)
	}
	inactive, err := runtime.UnsubscribeMCPResource(context.Background(), subscription.ID)
	if err != nil {
		t.Fatalf("UnsubscribeMCPResource() error = %v", err)
	}
	if inactive.State != "inactive" {
		t.Fatalf("inactive = %#v", inactive)
	}
}

func TestRuntimeMCPSubscriptionSurvivesCanceledRequestContext(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "resources": [{"uri":"docs://readme","name":"README","description":"Readme"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))
	ctx, cancel := context.WithCancel(context.Background())

	subscription, err := runtime.SubscribeMCPResource(ctx, MCPSubscribeInput{URI: "docs://readme"})
	if err != nil {
		t.Fatalf("SubscribeMCPResource() error = %v", err)
	}
	cancel()
	if subscription.State != "active" {
		t.Fatalf("subscription = %#v", subscription)
	}
	time.Sleep(100 * time.Millisecond)
	runtime.mu.RLock()
	session := runtime.mcpSessions[subscription.ServerID]
	closed := session == nil || session.isClosed()
	runtime.mu.RUnlock()
	if closed {
		t.Fatalf("persistent MCP session closed after request context cancellation")
	}
	if _, err := runtime.UnsubscribeMCPResource(context.Background(), subscription.ID); err != nil {
		t.Fatalf("UnsubscribeMCPResource() error = %v", err)
	}
}

func TestRuntimeShutdownStopsActiveMCPSessions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "resources": [{"uri":"docs://readme","name":"README","description":"Readme"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	subscription, err := runtime.SubscribeMCPResource(context.Background(), MCPSubscribeInput{URI: "docs://readme"})
	if err != nil {
		t.Fatalf("SubscribeMCPResource() error = %v", err)
	}
	if subscription.State != "active" {
		t.Fatalf("subscription = %#v", subscription)
	}
	if len(runtime.mcpSessions) != 1 {
		t.Fatalf("mcpSessions = %#v", runtime.mcpSessions)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if len(runtime.mcpSessions) != 0 {
		t.Fatalf("mcpSessions = %#v", runtime.mcpSessions)
	}
	subscriptions := runtime.ListMCPSubscriptions(context.Background())
	if len(subscriptions) != 1 || subscriptions[0].State != "inactive" {
		t.Fatalf("subscriptions = %#v", subscriptions)
	}
}

func TestRuntimeAutoAgentLoopInfersMCPToolRequiredArguments(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "tools": [{"name":"echo","description":"Echo","inputSchema":"{\"type\":\"object\",\"required\":[\"message\"],\"properties\":{\"message\":{\"type\":\"string\"}}}"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: `use mcp tool echo with message "hello mcp"`,
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loopHasStep(loop, "mcp_boundary", "completed") || !loopHasStep(loop, "mcp_call", "completed") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	if calls := runtime.ListMCPCalls(context.Background()); len(calls) != 1 || calls[0].ToolID != "docs/echo" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRuntimeAutoAgentLoopStopsWhenMCPRequiredArgumentCannotBeInferred(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "tools": [{"name":"echo","description":"Echo","inputSchema":"{\"type\":\"object\",\"required\":[\"message\"],\"properties\":{\"message\":{\"type\":\"string\"}}}"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "use mcp tool echo",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || !loopHasStep(loop, "mcp_boundary", "completed") || loopHasStep(loop, "mcp_call", "completed") {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
	if calls := runtime.ListMCPCalls(context.Background()); len(calls) != 0 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRuntimeAutoAgentLoopRunsMultiMCPPlan(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"},
      "tools": [{"name":"ping","description":"Ping","inputSchema":"{\"type\":\"object\"}"}],
      "resources": [{"uri":"docs://readme","name":"README","description":"Readme"}],
      "prompts": [{"name":"review","description":"Review"}]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "all mcp actions",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" {
		t.Fatalf("loop = %#v", loop)
	}
	calls := runtime.ListMCPCalls(context.Background())
	if len(calls) != 3 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRuntimeAgentLoopCreatesEditProposal(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "old\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:            "change notes",
		ProposalPath:    "notes.md",
		ProposalContent: "new\n",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !loopHasStep(loop, "edit_proposal", "completed") || !loopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "notes.md" || proposals[0].Status != "pending" || proposals[0].Content != "new\n" {
		t.Fatalf("proposals = %#v", proposals)
	}
	if !loopHasCreatedID(loop, "edit_proposal", proposals[0].ID) {
		t.Fatalf("loop steps = %#v", loop.Steps)
	}
}

func TestRuntimeAgentLoopRejectedProposalDoesNotSatisfyEditBoundary(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	writeTestFile(t, filepath.Join(root, "notes.md"), "old\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"make test"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:            "change notes and run tests",
		ProposalPath:    "notes.md",
		ProposalContent: "new\n",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 {
		t.Fatalf("proposals = %#v", proposals)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposals[0].ID, EditProposalReviewInput{Status: "rejected"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	rejected, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop(reject) error = %v", err)
	}
	if !loopHasStep(rejected, "edit_review", "rejected") || loopHasStep(rejected, "command_run", "completed") {
		t.Fatalf("rejected = %#v", rejected)
	}

	continued, err := runtime.ContinueAgentLoop(context.Background(), loop.ID, AgentLoopContinueInput{Command: "make test"})
	if err != nil {
		t.Fatalf("ContinueAgentLoop(command) error = %v", err)
	}
	if loopHasStep(continued, "command_run", "completed") || loopHasStep(continued, "command_approval", "waiting_approval") {
		t.Fatalf("continued past rejected edit = %#v", continued)
	}
	content, err := os.ReadFile(filepath.Join(root, "notes.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "old\n" {
		t.Fatalf("file content = %q, want old", string(content))
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

func TestRuntimeAgentLoopFiresHooks(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "fix and test the project",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopHasStep(loop, "hook_before_tool", "completed") {
		t.Fatalf("loop missing before_tool hook step: %#v", loop)
	}
	if !loopHasStep(loop, "hook_after_check", "completed") {
		t.Fatalf("loop missing after_check hook step: %#v", loop)
	}
	hooks := runtime.ListHookRuns(context.Background())
	if len(hooks) < 2 {
		t.Fatalf("expected at least 2 hook runs, got %d: %#v", len(hooks), hooks)
	}
	hasBeforeTool := false
	hasAfterCheck := false
	for _, h := range hooks {
		if h.HookID == "before_tool" {
			hasBeforeTool = true
		}
		if h.HookID == "after_check" {
			hasAfterCheck = true
		}
	}
	if !hasBeforeTool {
		t.Fatal("missing before_tool hook run")
	}
	if !hasAfterCheck {
		t.Fatal("missing after_check hook run")
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
      "tools": [{"name": "search_docs", "description": "Search docs", "inputSchema": {"type":"object"}}]
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
	if tool.InputSchema != `{"type":"object"}` {
		t.Fatalf("mcp tool schema = %#v", tool)
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

func TestRuntimeDiscoversMCPTools(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	tools := runtime.ListMCPTools(context.Background())
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	if tools[0].ID != "docs/ping" || tools[0].Name != "ping" || tools[0].Description != "Ping" || tools[0].State != "ready" {
		t.Fatalf("tool = %#v", tools[0])
	}
}

func TestRuntimeDiscoversPagedMCPTools(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1","LINEA_FAKE_MCP_PAGED_TOOLS":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	tools := runtime.ListMCPTools(context.Background())
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", tools)
	}
	if tools[0].ID != "docs/lookup" || tools[1].ID != "docs/ping" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestStatusDiscoversMCPTools(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	status := runtime.Status(context.Background())

	if len(status.MCPServers) != 1 || status.MCPServers[0].State != "ready" {
		t.Fatalf("mcp servers = %#v", status.MCPServers)
	}
	if len(status.MCPTools) == 0 {
		t.Fatalf("mcp tools should be discovered, got empty: %#v", status.MCPTools)
	}
	if status.MCPTools[0].Name != "ping" {
		t.Fatalf("mcp tools = %#v", status.MCPTools)
	}
}

func TestRuntimeCallsDiscoveredMCPTool(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	call, err := runtime.CallMCPTool(context.Background(), MCPCallInput{ToolID: "docs/ping"})
	if err != nil {
		t.Fatalf("CallMCPTool() error = %v", err)
	}
	if call.State != "completed" || call.ToolID != "docs/ping" || !strings.Contains(call.Output, "pong") {
		t.Fatalf("call = %#v", call)
	}
}

func TestRuntimeCallsPagedDiscoveredMCPTool(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1","LINEA_FAKE_MCP_PAGED_TOOLS":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	call, err := runtime.CallMCPTool(context.Background(), MCPCallInput{ToolID: "docs/lookup"})
	if err != nil {
		t.Fatalf("CallMCPTool() error = %v", err)
	}
	if call.State != "completed" || call.ToolID != "docs/lookup" || !strings.Contains(call.Output, "pong") {
		t.Fatalf("call = %#v", call)
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

func TestRuntimeDiscoversReadsMCPResources(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	resources := runtime.ListMCPResources(context.Background())
	if len(resources) != 1 || resources[0].ID != "docs/"+mcpURIID("docs://readme") || resources[0].URI != "docs://readme" {
		t.Fatalf("resources = %#v", resources)
	}
	call, err := runtime.ReadMCPResource(context.Background(), MCPResourceReadInput{ResourceID: resources[0].ID})
	if err != nil {
		t.Fatalf("ReadMCPResource() error = %v", err)
	}
	if call.State != "completed" || !strings.Contains(call.Output, "# README") {
		t.Fatalf("call = %#v", call)
	}
}

func TestRuntimeMCPServerRunsRelativeToConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	writeTestFile(t, filepath.Join(configDir, "server.sh"), "#!/bin/sh\nexec \""+os.Args[0]+"\"\n")
	if err := os.Chmod(filepath.Join(configDir, "server.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "./server.sh",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"}
    }
  }
}`)
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	tools := runtime.ListMCPTools(context.Background())
	if len(tools) != 1 || tools[0].ID != "docs/ping" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestRuntimeMCPResourceIDsPreserveExtensions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "node",
      "resources": [
        {"uri":"file:///repo/README.md","name":"README md"},
        {"uri":"file:///repo/README.txt","name":"README txt"}
      ]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	resources := runtime.ListMCPResources(context.Background())
	if len(resources) != 2 {
		t.Fatalf("resources = %#v", resources)
	}
	ids := map[string]bool{}
	for _, resource := range resources {
		ids[resource.ID] = true
	}
	hasMD := false
	hasTXT := false
	for id := range ids {
		hasMD = hasMD || strings.HasPrefix(id, "docs/file_repo_readme_md_")
		hasTXT = hasTXT || strings.HasPrefix(id, "docs/file_repo_readme_txt_")
	}
	if !hasMD || !hasTXT {
		t.Fatalf("resource ids = %#v", resources)
	}
}

func TestRuntimeMCPResourceIDsAvoidPunctuationCollisions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "node",
      "resources": [
        {"uri":"docs://foo-bar","name":"dash"},
        {"uri":"docs://foo_bar","name":"underscore"}
      ]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	resources := runtime.ListMCPResources(context.Background())
	if len(resources) != 2 {
		t.Fatalf("resources = %#v", resources)
	}
	ids := map[string]bool{}
	for _, resource := range resources {
		if ids[resource.ID] {
			t.Fatalf("duplicate resource id %q in %#v", resource.ID, resources)
		}
		ids[resource.ID] = true
	}
}

func TestRuntimeDiscoversGetsMCPPrompts(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_MCP_SERVER":"1"}
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	prompts := runtime.ListMCPPrompts(context.Background())
	if len(prompts) != 1 || prompts[0].ID != "docs/"+mcpPromptID("review") || prompts[0].Name != "review" {
		t.Fatalf("prompts = %#v", prompts)
	}
	call, err := runtime.GetMCPPrompt(context.Background(), MCPPromptGetInput{PromptID: prompts[0].ID})
	if err != nil {
		t.Fatalf("GetMCPPrompt() error = %v", err)
	}
	if call.State != "completed" || !strings.Contains(call.Output, "Review this") {
		t.Fatalf("call = %#v", call)
	}
}

func TestRuntimeMCPPromptIDsAvoidPunctuationCollisions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeTestFile(t, configPath, `{
  "mcpServers": {
    "docs": {
      "command": "node",
      "prompts": [
        {"name":"review-code","description":"dash"},
        {"name":"review_code","description":"underscore"}
      ]
    }
  }
}`)
	runtime := NewRuntime("", WithMCPConfigPath(configPath))

	prompts := runtime.ListMCPPrompts(context.Background())
	if len(prompts) != 2 {
		t.Fatalf("prompts = %#v", prompts)
	}
	ids := map[string]bool{}
	for _, prompt := range prompts {
		if ids[prompt.ID] {
			t.Fatalf("duplicate prompt id %q in %#v", prompt.ID, prompts)
		}
		ids[prompt.ID] = true
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
	if continued.Steps[len(continued.Steps)-3].Detail != "1 diagnostic(s) after command" {
		t.Fatalf("diagnostics step = %#v", continued.Steps[len(continued.Steps)-3])
	}
	if continued.Steps[len(continued.Steps)-2].Kind != "diagnostics_review" || continued.Steps[len(continued.Steps)-2].State != "attention" {
		t.Fatalf("diagnostics review step = %#v", continued.Steps[len(continued.Steps)-2])
	}
	if continued.Steps[len(continued.Steps)-1].Kind != "retry" || continued.Steps[len(continued.Steps)-1].State != "waiting_input" {
		t.Fatalf("retry step = %#v", continued.Steps[len(continued.Steps)-1])
	}

	retried, err := runtime.ContinueAgentLoop(context.Background(), continued.ID, AgentLoopContinueInput{
		ProposalPath:    "broken.go",
		ProposalContent: "package main\nfunc fixed() {}\n",
	})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() retry error = %v", err)
	}
	if retried.State != "waiting_approval" || !loopHasStep(retried, "edit_proposal", "completed") || !loopHasStep(retried, "edit_review", "waiting_approval") {
		t.Fatalf("retried = %#v", retried)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "broken.go" {
		t.Fatalf("proposals = %#v", proposals)
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
	if continued.State != "attention" || !loopHasStep(continued, "command_run", "blocked") || !loopHasStep(continued, "retry", "waiting_input") {
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

func TestRuntimeRunsBoundedSubagentPlan(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "agent notes\n")
	writeTestFile(t, filepath.Join(root, "README.md"), "agent docs\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	plan, err := runtime.RunSubagentPlan(context.Background(), SubagentPlanInput{
		Goal:  "review docs and search",
		Query: "agent",
	})
	if err != nil {
		t.Fatalf("RunSubagentPlan() error = %v", err)
	}
	if plan.State != "completed" || len(plan.Runs) != 3 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.SubagentIDs[0] != "review" || plan.SubagentIDs[1] != "docs" || plan.SubagentIDs[2] != "search" {
		t.Fatalf("plan ids = %#v", plan.SubagentIDs)
	}
	runs := runtime.ListSubagentRuns(context.Background())
	if len(runs) != 3 {
		t.Fatalf("runs = %#v", runs)
	}
	status := runtime.Status(context.Background())
	if len(status.SubagentPlans) != 1 || status.RunSummary.SubagentRuns != 3 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRuntimeSubagentPlanHonorsExplicitIDs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "notes.md"), "agent notes\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	plan, err := runtime.RunSubagentPlan(context.Background(), SubagentPlanInput{
		Goal:        "run selected helpers",
		Query:       "agent",
		SubagentIDs: []string{"search", "review", "search"},
	})
	if err != nil {
		t.Fatalf("RunSubagentPlan() error = %v", err)
	}
	if len(plan.SubagentIDs) != 2 || plan.SubagentIDs[0] != "search" || plan.SubagentIDs[1] != "review" {
		t.Fatalf("plan ids = %#v", plan.SubagentIDs)
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

func TestRuntimeUsesConfiguredLSPForDiagnosticsAndSymbols(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc Run() {}\nfunc main() { Run() }\n")
	lsp := fakeLSPCommand(t, root)
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithLSPCommand(lsp))

	diagnostics, err := runtime.ListDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("ListDiagnostics() error = %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Path != "main.go" || diagnostics[0].Message != "lsp diagnostic" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	symbols, err := runtime.ListSymbols(context.Background(), "run")
	if err != nil {
		t.Fatalf("ListSymbols() error = %v", err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Run" || symbols[0].Kind != "func" || symbols[0].Path != "main.go" {
		t.Fatalf("symbols = %#v", symbols)
	}

}

func TestRuntimeAutoDetectsGoplsWhenAvailable(t *testing.T) {
	original := lspLookPath
	t.Cleanup(func() { lspLookPath = original })
	lspLookPath = func(command string) (string, error) {
		if command != "gopls" {
			return "", errors.New("unexpected command")
		}
		return "/usr/local/bin/gopls", nil
	}

	runtime := NewRuntime("")
	if runtime.lspCommand != "/usr/local/bin/gopls" {
		t.Fatalf("lsp command = %q", runtime.lspCommand)
	}
}

func TestRuntimeExplicitLSPDisableSkipsAutoDetect(t *testing.T) {
	original := lspLookPath
	t.Cleanup(func() { lspLookPath = original })
	lspLookPath = func(command string) (string, error) {
		if command != "gopls" {
			return "", errors.New("unexpected command")
		}
		return "/usr/local/bin/gopls", nil
	}

	runtime := NewRuntime("", WithLSPCommand("off"))
	if runtime.lspCommand != "off" || runtime.lspConfigured() {
		t.Fatalf("lsp command = %q configured=%v", runtime.lspCommand, runtime.lspConfigured())
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

func TestRuntimeUsesConfiguredLSPForNameBasedReferences(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), "package main\n\nfunc Run() {}\nfunc main() { Run() }\n")
	writeTestFile(t, filepath.Join(root, "b.go"), "package main\n\nfunc Run() {}\nfunc other() { Run() }\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithLSPCommand(fakeLSPCommand(t, root)))

	references, err := runtime.ListReferences(context.Background(), "Run")
	if err != nil {
		t.Fatalf("ListReferences() error = %v", err)
	}
	if len(references) != 4 {
		t.Fatalf("references = %#v", references)
	}
	if references[0].Path != "a.go" || references[2].Path != "b.go" {
		t.Fatalf("references = %#v", references)
	}
}

func TestLSPReferencesSkipPathsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc Run() {}\n")
	writeTestFile(t, filepath.Join(outside, "external.go"), "package external\n\nfunc Run() {}\n")

	output := []byte(filepath.Join(outside, "external.go") + ":3:6-9\nmain.go:3:6-9\n")
	references := parseLSPReferences(root, output, "Run")
	if len(references) != 1 {
		t.Fatalf("references = %#v", references)
	}
	if references[0].Path != "main.go" || !strings.Contains(references[0].Text, "func Run") {
		t.Fatalf("references = %#v", references)
	}
}

func TestLSPReferencesSkipSymlinkTargetsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc Run() {}\n")
	writeTestFile(t, filepath.Join(outside, "external.go"), "package external\n\nfunc Run() {}\n")
	if err := os.Symlink(filepath.Join(outside, "external.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	output := []byte("link.go:3:6-9\nmain.go:3:6-9\n")
	references := parseLSPReferences(root, output, "Run")
	if len(references) != 1 {
		t.Fatalf("references = %#v", references)
	}
	if references[0].Path != "main.go" {
		t.Fatalf("references = %#v", references)
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

func TestWorkspaceFullTrustReadsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "notes.txt")
	writeTestFile(t, outside, "outside")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithDeveloperMode(true, "full"))

	result, err := runtime.ReadFile(context.Background(), outside)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != resolvedOutside || result.Content != "outside" {
		t.Fatalf("result = %#v", result)
	}
}

func TestWorkspaceFullTrustDeveloperReadKeepsRelativeWorkspacePath(t *testing.T) {
	processDir := t.TempDir()
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(processDir, "README.md"), "process")
	writeTestFile(t, filepath.Join(workspace, "README.md"), "workspace")
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(processDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("Chdir() restore error = %v", err)
		}
	}()
	runtime := NewRuntime("", WithWorkspaceRoot(workspace), WithDeveloperMode(true, "full"))

	result, err := runtime.ReadDeveloperFile(context.Background(), "README.md")
	if err != nil {
		t.Fatalf("ReadDeveloperFile() error = %v", err)
	}
	if result.Path != "README.md" || result.Content != "workspace" {
		t.Fatalf("result = %#v", result)
	}
}

func TestWorkspaceFiltersSecretFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".env"), "API_KEY=secret\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithDeveloperMode(true, "full"))

	if _, err := runtime.ReadFile(context.Background(), ".env"); err == nil {
		t.Fatal("ReadFile(.env) error = nil, want secret filter")
	}
	if _, err := runtime.ProposeEdit(context.Background(), EditProposalInput{Path: ".env", Content: "API_KEY=next\n"}); err == nil {
		t.Fatal("ProposeEdit(.env) error = nil, want secret filter")
	}
	results, err := runtime.SearchFiles(context.Background(), "secret")
	if err != nil {
		t.Fatalf("SearchFiles() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
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

func TestProposeEditCanCreateNewFile(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "portfolio.html",
		Content: "<!doctype html>\n",
		Summary: "Create portfolio",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if proposal.Status != "pending" || proposal.Path != "portfolio.html" {
		t.Fatalf("proposal = %#v", proposal)
	}
	if _, err := os.Stat(filepath.Join(root, "portfolio.html")); !os.IsNotExist(err) {
		t.Fatalf("new file exists before apply, err = %v", err)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err != nil {
		t.Fatalf("ApplyEditProposal() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "portfolio.html"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "<!doctype html>\n" {
		t.Fatalf("content = %q", string(content))
	}
}

func TestProposeEditCanCreateNewFileInMissingDirectory(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "src/components/Card.jsx",
		Content: "export default function Card() { return null; }\n",
		Summary: "Create card",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if proposal.Status != "pending" || proposal.Path != "src/components/Card.jsx" {
		t.Fatalf("proposal = %#v", proposal)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err != nil {
		t.Fatalf("ApplyEditProposal() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "src", "components", "Card.jsx"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "export default function Card() { return null; }\n" {
		t.Fatalf("content = %q", string(content))
	}
}

func TestApplyNewFileProposalRejectsStaleFile(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	proposal, err := runtime.ProposeEdit(context.Background(), EditProposalInput{
		Path:    "portfolio.html",
		Content: "<!doctype html>\n",
	})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	writeTestFile(t, filepath.Join(root, "portfolio.html"), "changed\n")
	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err == nil {
		t.Fatal("ApplyEditProposal() error = nil, want stale proposal error")
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

func TestApplyEditProposalRejectsDeletedEmptyBase(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "notes.md")
	writeTestFile(t, filePath, "")
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
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err == nil {
		t.Fatal("ApplyEditProposal() error = nil, want stale error")
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file stat err = %v, want not exist", err)
	}
}

func TestApplyEditProposalRejectsCreateAfterEmptyFileAppears(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "notes.md")
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
	writeTestFile(t, filePath, "")

	if _, err := runtime.ApplyEditProposal(context.Background(), proposal.ID); err == nil {
		t.Fatal("ApplyEditProposal() error = nil, want stale error")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "" {
		t.Fatalf("file content = %q, want empty", string(content))
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

func TestRuntimeAutoLoopCreatesTempReactPreview(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-1",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" {
		t.Fatalf("loop.State = %q, want completed", loop.State)
	}
	if loop.WorkspaceRoot == "" || loop.WorkspaceRoot == root {
		t.Fatalf("loop.WorkspaceRoot = %q, root = %q", loop.WorkspaceRoot, root)
	}
	if !strings.HasPrefix(loop.PreviewURL, "/api/agent/previews/") {
		t.Fatalf("loop.PreviewURL = %q", loop.PreviewURL)
	}
	if _, err := os.Stat(filepath.Join(root, "react-page.html")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("react-page.html in workspace err = %v, want not exist", err)
	}
	previewID := strings.Trim(strings.TrimPrefix(loop.PreviewURL, "/api/agent/previews/"), "/")
	file, err := runtime.PreviewFile(context.Background(), previewID, "")
	if err != nil {
		t.Fatalf("PreviewFile() error = %v", err)
	}
	if file.Path != "index.html" || !strings.Contains(string(file.Content), "./assets/app.js") {
		t.Fatalf("preview file = %#v", file)
	}
	if strings.Contains(string(file.Content), "https://") || !strings.Contains(string(file.Content), "./assets/app.css") {
		t.Fatalf("preview should use local React assets: %s", string(file.Content))
	}
	reactShim, err := runtime.PreviewFile(context.Background(), previewID, "vendor/react.js")
	if err != nil {
		t.Fatalf("PreviewFile(React shim) error = %v", err)
	}
	if !strings.Contains(string(reactShim.Content), "createElement") || !strings.Contains(string(reactShim.Content), "useState") {
		t.Fatalf("react shim = %s", string(reactShim.Content))
	}
	appFile, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(App) error = %v", err)
	}
	if !strings.Contains(string(appFile.Content), "export default function App") {
		t.Fatalf("app file = %#v", appFile)
	}
	if !loopHasStep(loop, "preview", "completed") {
		t.Fatalf("loop missing preview step: %#v", loop.Steps)
	}
	if loopHasStep(loop, "command_run", "completed") {
		t.Fatalf("temp preview should not require shell commands: %#v", loop.Steps)
	}

	edited, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "edit the app and make it louder",
		Mode:          "auto",
		TempWorkspace: true,
		SessionID:     "chat-1",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(edit) error = %v", err)
	}
	if edited.WorkspaceRoot != loop.WorkspaceRoot {
		t.Fatalf("edited.WorkspaceRoot = %q, want %q", edited.WorkspaceRoot, loop.WorkspaceRoot)
	}
	editedPreviewID := strings.Trim(strings.TrimPrefix(edited.PreviewURL, "/api/agent/previews/"), "/")
	editedApp, err := runtime.PreviewFile(context.Background(), editedPreviewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(edited App) error = %v", err)
	}
	if !strings.Contains(string(editedApp.Content), "linear-gradient") {
		t.Fatalf("edited app content = %s", string(editedApp.Content))
	}
	originalApp, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(original App) error = %v", err)
	}
	if strings.Contains(string(originalApp.Content), "linear-gradient") {
		t.Fatalf("original preview changed = %s", string(originalApp.Content))
	}
}

func TestRuntimeAutoLoopKeepsCurrentWorkspaceReactRequestsInWorkspace(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "create a React app in the current workspace",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.PreviewURL != "" || loop.WorkspaceRoot != "" {
		t.Fatalf("loop used temp preview path = %#v", loop)
	}
	if !loopHasStep(loop, "edit_boundary", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}
	runtime.mu.RLock()
	sessionCount := len(runtime.appSessions)
	runtime.mu.RUnlock()
	if sessionCount != 0 {
		t.Fatalf("app sessions = %d", sessionCount)
	}
}

func TestRuntimeAutoLoopAcceptsPlannerDefaultExportModule(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from 'react';

const App = () => React.createElement("h1", null, "Hi");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-planner",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" {
		t.Fatalf("loop = %#v", loop)
	}
	previewID := strings.Trim(strings.TrimPrefix(loop.PreviewURL, "/api/agent/previews/"), "/")
	appFile, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(App) error = %v", err)
	}
	if !strings.Contains(string(appFile.Content), "export default App") {
		t.Fatalf("app file = %s", string(appFile.Content))
	}
}

func TestRuntimeAutoLoopAcceptsPlannerJSXModule(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";
import "./App.css";

export default function App() {
  return <main><h1>Hi</h1></main>;
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-jsx-planner",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" || !loopHasStep(loop, "preview", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
	previewID := strings.Trim(strings.TrimPrefix(loop.PreviewURL, "/api/agent/previews/"), "/")
	bundle, err := runtime.PreviewFile(context.Background(), previewID, "assets/app.js")
	if err != nil {
		t.Fatalf("PreviewFile(bundle) error = %v", err)
	}
	if !strings.Contains(string(bundle.Content), "createElement") {
		t.Fatalf("bundle = %s", string(bundle.Content))
	}
}

func TestRuntimeAutoLoopRejectsBrokenPlannerModuleBeforePreview(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `const App = () => React.createElement("h1", null, "Hi");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-broken-planner",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || loop.PreviewURL != "" || !loopHasStep(loop, "app_check", "blocked") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopRestoresLastGoodTempAppAfterFailedEdit(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

const App = () => React.createElement("h1", null, "Good");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	created, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-restore",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(create) error = %v", err)
	}
	if created.State != "completed" {
		t.Fatalf("created = %#v", created)
	}
	session, ok := runtime.appSession("chat-restore")
	if !ok {
		t.Fatal("app session was not saved")
	}
	original, err := os.ReadFile(filepath.Join(session.Root, "src", "App.jsx"))
	if err != nil {
		t.Fatalf("read original App.jsx: %v", err)
	}

	planner.plan = EditPlan{
		Path: "src/App.jsx",
		Content: `const App = () => React.createElement("h1", null, "Broken");

export default App;
`,
		Summary: "Break app",
	}
	failed, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "edit the app",
		Mode:          "auto",
		TempWorkspace: true,
		SessionID:     "chat-restore",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(broken edit) error = %v", err)
	}
	if failed.State != "attention" || failed.PreviewURL != "" || !loopHasStep(failed, "app_check", "blocked") {
		t.Fatalf("failed = %#v", failed)
	}
	restored, err := os.ReadFile(filepath.Join(session.Root, "src", "App.jsx"))
	if err != nil {
		t.Fatalf("read restored App.jsx: %v", err)
	}
	if string(restored) != string(original) {
		t.Fatalf("source was not restored:\n%s", string(restored))
	}

	planner.plan = EditPlan{
		Path: "src/App.jsx",
		Content: `import React from "react";

const App = () => React.createElement("h1", null, "Recovered");

export default App;
`,
		Summary: "Recover app",
	}
	recovered, err := runtime.ContinueAgentLoop(context.Background(), failed.ID, AgentLoopContinueInput{
		Query: "edit the app",
	})
	if err != nil {
		t.Fatalf("ContinueAgentLoop(recover edit) error = %v", err)
	}
	if recovered.State != "completed" || recovered.PreviewURL == "" {
		t.Fatalf("recovered = %#v", recovered)
	}
	if recovered.WorkspaceRoot != session.Root {
		t.Fatalf("recovered.WorkspaceRoot = %q, want %q", recovered.WorkspaceRoot, session.Root)
	}
	previewID := strings.Trim(strings.TrimPrefix(recovered.PreviewURL, "/api/agent/previews/"), "/")
	recoveredApp, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(recovered App) error = %v", err)
	}
	if !strings.Contains(string(recoveredApp.Content), "Recovered") {
		t.Fatalf("recovered app = %s", string(recoveredApp.Content))
	}
}

func TestRuntimeAutoLoopKeepsTempAppWhenPlannerCannotEdit(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

const App = () => React.createElement("h1", null, "Original");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	created, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-keep",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(create) error = %v", err)
	}
	if created.State != "completed" {
		t.Fatalf("created = %#v", created)
	}
	session, ok := runtime.appSession("chat-keep")
	if !ok {
		t.Fatal("app session was not saved")
	}
	original, err := os.ReadFile(filepath.Join(session.Root, "src", "App.jsx"))
	if err != nil {
		t.Fatalf("read original App.jsx: %v", err)
	}

	planner.plan = EditPlan{
		Path:    "src/App.jsx",
		Content: "not a react module",
		Summary: "Invalid edit",
	}
	edited, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "turn the app into a database",
		Mode:          "auto",
		TempWorkspace: true,
		SessionID:     "chat-keep",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(edit) error = %v", err)
	}
	if edited.State != "attention" || edited.PreviewURL != "" || !loopHasStep(edited, "write_file", "blocked") {
		t.Fatalf("edited = %#v", edited)
	}
	restored, err := os.ReadFile(filepath.Join(session.Root, "src", "App.jsx"))
	if err != nil {
		t.Fatalf("read restored App.jsx: %v", err)
	}
	if string(restored) != string(original) {
		t.Fatalf("app source changed:\n%s", string(restored))
	}
}

func TestRuntimeAutoLoopUsesColorFallbackWhenPlannerCannotEdit(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

export default function App() {
  return <main>Hello bird</main>;
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	created, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a react app which says hello bird",
		Mode:      "auto",
		SessionID: "chat-color-fallback",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(create) error = %v", err)
	}
	if created.State != "completed" {
		t.Fatalf("created = %#v", created)
	}

	planner.plan = EditPlan{
		Path:    "src/App.jsx",
		Content: "not a react module",
		Summary: "Invalid edit",
	}
	edited, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "make it blue",
		Mode:          "auto",
		TempWorkspace: true,
		SessionID:     "chat-color-fallback",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(edit) error = %v", err)
	}
	if edited.State != "completed" || edited.PreviewURL == "" {
		t.Fatalf("edited = %#v", edited)
	}
	previewID := strings.Trim(strings.TrimPrefix(edited.PreviewURL, "/api/agent/previews/"), "/")
	app, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(App) error = %v", err)
	}
	content := string(app.Content)
	if !strings.Contains(content, "#1d4ed8") || !strings.Contains(content, "Hello bird") {
		t.Fatalf("edited app = %s", content)
	}
}

func TestRuntimeAutoLoopEditsExistingTempAppWithPlanner(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

export default function App() {
  return React.createElement("h1", null, "Hello bird");
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	created, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a react app which says hello bird",
		Mode:      "auto",
		SessionID: "chat-edit-app",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(create) error = %v", err)
	}
	if created.State != "completed" {
		t.Fatalf("created = %#v", created)
	}
	session, ok := runtime.appSession("chat-edit-app")
	if !ok {
		t.Fatal("app session was not saved")
	}

	planner.plan = EditPlan{
		Path: "src/App.jsx",
		Content: `import React from "react";

export default function App() {
  return React.createElement("main", { style: {
    minHeight: "100vh",
    display: "grid",
    placeItems: "center",
    background: "#1d4ed8",
    color: "white",
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
  } }, React.createElement("h1", null, "Hello bird"));
}
`,
		Summary: "Make background blue",
	}
	edited, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "make the app background blue",
		Mode:          "auto",
		TempWorkspace: true,
		SessionID:     "chat-edit-app",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(edit) error = %v", err)
	}
	if edited.State != "completed" || edited.PreviewURL == "" {
		t.Fatalf("edited = %#v", edited)
	}
	if edited.WorkspaceRoot != session.Root {
		t.Fatalf("edited.WorkspaceRoot = %q, want %q", edited.WorkspaceRoot, session.Root)
	}
	previewID := strings.Trim(strings.TrimPrefix(edited.PreviewURL, "/api/agent/previews/"), "/")
	app, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(App) error = %v", err)
	}
	content := string(app.Content)
	if !strings.Contains(content, "#1d4ed8") || !strings.Contains(content, "Hello bird") {
		t.Fatalf("edited app = %s", content)
	}
	if len(planner.requests) < 2 || planner.requests[1].Files[0].Path != "src/App.jsx" || !strings.Contains(planner.requests[1].Files[0].Content, "Hello bird") {
		t.Fatalf("planner requests = %#v", planner.requests)
	}
}

func TestRuntimeAutoLoopCreatePromptReplacesExistingTempApp(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

export default function App() {
  return React.createElement("h1", null, "First");
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	created, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-recreate",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(create) error = %v", err)
	}
	if created.State != "completed" {
		t.Fatalf("created = %#v", created)
	}
	firstSession, ok := runtime.appSession("chat-recreate")
	if !ok {
		t.Fatal("first app session was not saved")
	}

	planner.plan = EditPlan{
		Path: "src/App.jsx",
		Content: `import React from "react";

export default function App() {
  return React.createElement("h1", null, "Hello");
}
`,
		Summary: "Create hello app",
	}
	recreated, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:          "create a react app which says hello",
		Mode:          "auto",
		TempWorkspace: true,
		SessionID:     "chat-recreate",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop(recreate) error = %v", err)
	}
	if recreated.State != "completed" || recreated.PreviewURL == "" || !loopHasStep(recreated, "workspace", "completed") {
		t.Fatalf("recreated = %#v", recreated)
	}
	secondSession, ok := runtime.appSession("chat-recreate")
	if !ok {
		t.Fatal("second app session was not saved")
	}
	if secondSession.Root == firstSession.Root {
		t.Fatalf("session root was reused: %q", secondSession.Root)
	}
	app, err := os.ReadFile(filepath.Join(secondSession.Root, "src", "App.jsx"))
	if err != nil {
		t.Fatalf("read recreated App.jsx: %v", err)
	}
	if !strings.Contains(string(app), "Hello") {
		t.Fatalf("recreated app = %s", string(app))
	}
	if _, err := os.Stat(firstSession.Root); !os.IsNotExist(err) {
		t.Fatalf("old session root still exists or stat failed: %v", err)
	}
}

func TestRuntimeAutoLoopUsesPromptMessageFallbackForTempApp(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path:    "src/App.jsx",
			Content: "not a react module",
			Summary: "Unsupported app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a react app which says welcome",
		Mode:      "auto",
		SessionID: "chat-message-fallback",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" {
		t.Fatalf("loop = %#v", loop)
	}
	previewID := strings.Trim(strings.TrimPrefix(loop.PreviewURL, "/api/agent/previews/"), "/")
	app, err := runtime.PreviewFile(context.Background(), previewID, "src/App.jsx")
	if err != nil {
		t.Fatalf("PreviewFile(App) error = %v", err)
	}
	if !strings.Contains(string(app.Content), `"welcome"`) {
		t.Fatalf("app = %s", string(app.Content))
	}
}

func TestRuntimeAutoLoopRejectsMalformedPlannerModuleBeforePreview(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

const App = () => React.createElement("h1",, "Hi");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-malformed-planner",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || loop.PreviewURL != "" || !loopHasStep(loop, "app_check", "blocked") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopAcceptsPlannerCSSImportsBeforePreview(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";
import "./App.css";

const App = () => React.createElement("h1", null, "Hi");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-relative-import",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" || !loopHasStep(loop, "preview", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopRejectsPlannerPackageImportsBeforePreview(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";
import { Link } from "react-router-dom";

const App = () => React.createElement(Link, { to: "/" }, "Hi");

export default App;
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-package-import",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || loop.PreviewURL != "" || !loopHasStep(loop, "app_check", "blocked") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopRejectsMultiplePlannerPackageImportsOnOneLine(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react"; import confetti from "canvas-confetti";

export default function App() {
  return React.createElement("button", { onClick: confetti }, "Hi");
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-multiple-imports",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "attention" || loop.PreviewURL != "" || !loopHasStep(loop, "app_check", "blocked") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopAcceptsCommonReactNamedImportsBeforePreview(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React, { useEffect } from "react";

export default function App() {
  useEffect(() => {}, []);
  return React.createElement("h1", null, "Hi");
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-unsupported-react-import",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" || !loopHasStep(loop, "preview", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopAcceptsCommonReactDefaultAPIsBeforePreview(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React from "react";

export default function App() {
  React.useEffect(() => {}, []);
  return React.createElement("h1", null, "Hi");
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-unsupported-react-api",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" || !loopHasStep(loop, "preview", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRuntimeAutoLoopAcceptsSupportedReactNamedImports(t *testing.T) {
	root := t.TempDir()
	planner := &fakeEditPlanner{
		plan: EditPlan{
			Path: "src/App.jsx",
			Content: `import React, { useState } from "react";

export default function App() {
  const [label] = useState("Hi");
  return React.createElement("h1", null, label);
}
`,
			Summary: "Create app",
		},
	}
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(planner))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:      "create a React app",
		Mode:      "auto",
		SessionID: "chat-supported-react-import",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "completed" || loop.PreviewURL == "" || !loopHasStep(loop, "preview", "completed") {
		t.Fatalf("loop = %#v", loop)
	}
}

func TestRegisterAgentPreviewRemovesEvictedRoots(t *testing.T) {
	runtime := NewRuntime("")
	evictedRoot := ""
	for index := 0; index < maxAgentPreviewItems+1; index++ {
		root := t.TempDir()
		if index == 0 {
			evictedRoot = root
		}
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("ok"), 0o600); err != nil {
			t.Fatalf("write preview: %v", err)
		}
		runtime.registerAgentPreview("preview-"+strconv.Itoa(index), "loop", "session", root, "index.html")
	}
	if _, err := os.Stat(evictedRoot); !os.IsNotExist(err) {
		t.Fatalf("evicted root still exists, err = %v", err)
	}
}

func TestSaveAppSessionRemovesEvictedRoots(t *testing.T) {
	runtime := NewRuntime("")
	evictedRoot := ""
	for index := 0; index < maxAppSessionItems+1; index++ {
		root := t.TempDir()
		if index == 0 {
			evictedRoot = root
		}
		runtime.saveAppSession(AppSession{ID: "session-" + strconv.Itoa(index), Root: root})
	}
	if _, err := os.Stat(evictedRoot); !os.IsNotExist(err) {
		t.Fatalf("evicted app root still exists, err = %v", err)
	}
}

func TestRecoverAppSessionAcceptsExistingTempPackage(t *testing.T) {
	runtime := NewRuntime("")
	root, err := os.MkdirTemp("", "linea-app-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll(src) error = %v", err)
	}
	for name, content := range map[string]string{
		"package.json": "{}",
		"index.html":   "<main></main>",
		"src/App.jsx":  "export default function App() {}",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if !runtime.RecoverAppSession("chat", root) {
		t.Fatal("RecoverAppSession() returned false")
	}
	if !runtime.HasAppSession("chat") {
		t.Fatal("session was not recovered")
	}
}

func TestRecoverAppSessionRejectsNonTempRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll(src) error = %v", err)
	}
	for name, content := range map[string]string{
		"package.json": "{}",
		"index.html":   "<main></main>",
		"src/App.jsx":  "export default function App() {}",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	runtime := NewRuntime("")
	if runtime.RecoverAppSession("chat", root) {
		t.Fatal("RecoverAppSession() accepted non-temp app root")
	}
	if runtime.HasAppSession("chat") {
		t.Fatal("session was recovered unexpectedly")
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func fakeLSPCommand(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "fake-gopls")
	script := `#!/bin/sh
case "$1" in
  check)
    echo "main.go:2:1: lsp diagnostic"
    ;;
  workspace_symbol)
    echo "main.go:3:6: func Run"
    ;;
  references)
    if [ "$2" != "-d" ]; then
      exit 3
    fi
    case "$3" in
      a.go:3:*|a.go:4:*)
        echo "a.go:3:6-9"
        echo "a.go:4:15-18"
        ;;
      b.go:3:*|b.go:4:*)
        echo "b.go:3:6-9"
        echo "b.go:4:16-19"
        ;;
      *)
        echo "main.go:3:6-9"
        echo "main.go:4:15-18"
        ;;
    esac
    ;;
  *)
    exit 2
    ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake lsp: %v", err)
	}
	return path
}

func TestRuntimeDeveloperLoopInspectsGitFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "README.md"), "notes")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "false",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_followup", "git status --short") {
		t.Fatalf("expected git status inspection, steps = %#v", loop.Steps)
	}
	if !loopStepHasCommand(loop, "command_run", "git status --short") {
		t.Fatalf("expected git status run, steps = %#v", loop.Steps)
	}
}

func TestRuntimeDeveloperLoopInspectsMakeFailure(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not available on PATH")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Makefile"), "test:\n\tprintf ok\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "make nonexistent",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_followup", "sed -n 1,160p Makefile") {
		t.Fatalf("expected Makefile inspection, steps = %#v", loop.Steps)
	}
}

func TestRuntimeDeveloperLoopInspectsNpmFailure(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available on PATH")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"scripts":{}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "npm test",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_followup", "npm run") {
		t.Fatalf("expected npm run inspection, steps = %#v", loop.Steps)
	}
}

func TestRuntimeDeveloperLoopInspectsGoFailure(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available on PATH")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module test\n")
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc broken( {\n")
	runtime := NewRuntime("", WithWorkspaceRoot(root))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal:    "run command",
		Mode:    "developer",
		Command: "go build .",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_followup", "go env GOMOD") {
		t.Fatalf("expected go env GOMOD inspection, steps = %#v", loop.Steps)
	}
}

func TestRuntimeAutoLoopInfersPackageScriptTest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"scripts":{"test":"echo ok"}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"npm test"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "run tests",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_run", "npm test") {
		t.Fatalf("expected npm test, steps = %#v", loop.Steps)
	}
}

func TestRuntimeAutoLoopInfersPackageScriptLint(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"scripts":{"lint":"echo ok"}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"npm run lint"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "lint the project",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_run", "npm run lint") {
		t.Fatalf("expected npm run lint, steps = %#v", loop.Steps)
	}
}

func TestRuntimeAutoLoopInfersPackageScriptBuild(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"scripts":{"build":"echo ok"}}`)
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithCommandAllowlist([]string{"npm run build"}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "build the frontend",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if !loopStepHasCommand(loop, "command_run", "npm run build") {
		t.Fatalf("expected npm run build, steps = %#v", loop.Steps)
	}
}

func TestRuntimeConsumeAppliedEditReviewMissingProposal(t *testing.T) {
	runtime := NewRuntime("")
	loop := AgentLoop{
		ID:    "test-loop",
		State: "running",
		Steps: []AgentLoopStep{
			{
				Kind:      "edit_review",
				State:     "waiting_approval",
				CreatedID: "nonexistent",
			},
		},
	}
	result, blocked := runtime.consumeAppliedEditReviews(loop)
	if !blocked {
		t.Fatalf("expected blocked = true")
	}
	if result.State != "attention" {
		t.Fatalf("expected state = attention, got %q", result.State)
	}
	if result.Steps[0].State != "blocked" {
		t.Fatalf("expected step state = blocked, got %q", result.Steps[0].State)
	}
}

func TestAgentLoopAllowsNewFileCreation(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime("", WithWorkspaceRoot(root), WithEditPlanner(&fakeEditPlanner{
		plan: EditPlan{
			Path:    "new_file.go",
			Content: "package main\n",
			Summary: "Create new file",
		},
	}))

	loop, err := runtime.StartAgentLoop(context.Background(), AgentLoopInput{
		Goal: "create a new file named new_file.go",
		Mode: "auto",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}

	if loop.State != "waiting_approval" {
		t.Fatalf("loop.State = %q, want waiting_approval", loop.State)
	}

	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "new_file.go" {
		t.Fatalf("proposals = %#v", proposals)
	}
}

type fakeEditPlanner struct {
	plan     EditPlan
	plans    []EditPlan
	err      error
	requests []EditPlanRequest
}

func (f *fakeEditPlanner) PlanEdit(_ context.Context, request EditPlanRequest) (EditPlan, error) {
	f.requests = append(f.requests, request)
	if len(f.plans) >= len(f.requests) {
		return f.plans[len(f.requests)-1], f.err
	}
	return f.plan, f.err
}

func loopHasStep(loop AgentLoop, kind string, state string) bool {
	for _, step := range loop.Steps {
		if step.Kind == kind && step.State == state {
			return true
		}
	}
	return false
}

func loopStepHasCommand(loop AgentLoop, kind string, command string) bool {
	for _, step := range loop.Steps {
		if step.Kind == kind && step.Command == command {
			return true
		}
	}
	return false
}

func loopHasCreatedID(loop AgentLoop, kind string, createdID string) bool {
	for _, step := range loop.Steps {
		if step.Kind == kind && step.CreatedID == createdID {
			return true
		}
	}
	return false
}

func TestRuntimeAutoApproveCategoriesDefault(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	cats := runtime.AutoApproveCategories()
	if len(cats) != 0 {
		t.Fatalf("AutoApproveCategories() = %v, want empty", cats)
	}
}

func TestRuntimeAutoApproveCategoriesSetGet(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	runtime.SetAutoApproveCategories([]string{"read", "write"})
	cats := runtime.AutoApproveCategories()
	if len(cats) != 2 || cats[0] != "read" || cats[1] != "write" {
		t.Fatalf("AutoApproveCategories() = %v, want [read write]", cats)
	}
}

func TestRuntimeAutoApproveCategoriesClearsOnSetEmpty(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	runtime.SetAutoApproveCategories([]string{"read"})
	runtime.SetAutoApproveCategories(nil)
	cats := runtime.AutoApproveCategories()
	if len(cats) != 0 {
		t.Fatalf("AutoApproveCategories() = %v, want empty after clearing", cats)
	}
}

func TestRuntimeAutoApproveAllowsCommandByCategory(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test", "go test ./..."}), WithAutoApproveCategories([]string{"inspect"}))
	check, err := runtime.CheckCommand(context.Background(), CommandCheckInput{Command: "make test"})
	if err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	if !check.Allowed {
		t.Fatalf("CheckCommand().Allowed = false, want true")
	}
	if check.Reason != "auto-approved by category" {
		t.Fatalf("CheckCommand().Reason = %q, want %q", check.Reason, "auto-approved by category")
	}
}

func TestRuntimeAutoApproveBlocksCommandOutsideAllowlist(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}), WithAutoApproveCategories([]string{"read"}))
	check, err := runtime.CheckCommand(context.Background(), CommandCheckInput{Command: "printf ok"})
	if err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	if check.Allowed {
		t.Fatalf("CheckCommand().Allowed = true, want false")
	}
	if check.Reason != "not in allowlist" {
		t.Fatalf("CheckCommand().Reason = %q, want %q", check.Reason, "not in allowlist")
	}
}

func TestRuntimeAutoApproveCategoriesInitializedFromEnv(t *testing.T) {
	t.Setenv("LINEA_AUTO_APPROVE_CATEGORIES", "read,write")
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	cats := runtime.AutoApproveCategories()
	if len(cats) != 2 || cats[0] != "read" || cats[1] != "write" {
		t.Fatalf("AutoApproveCategories() = %v, want [read write]", cats)
	}
}

func TestRuntimeAutoApproveOptionOverridesEnv(t *testing.T) {
	t.Setenv("LINEA_AUTO_APPROVE_CATEGORIES", "read,write")
	runtime := NewRuntime("", WithAutoApproveCategories([]string{"inspect"}))
	cats := runtime.AutoApproveCategories()
	if len(cats) != 1 || cats[0] != "inspect" {
		t.Fatalf("AutoApproveCategories() = %v, want [inspect]", cats)
	}
}

func TestRuntimeAuditLogWritesEntry(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	runtime := NewRuntime("", WithAuditLogPath(logPath), WithCommandAllowlist([]string{"make test"}))
	_, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "make test", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "make test") {
		t.Fatalf("audit log missing command: %s", data)
	}
	if !strings.Contains(string(data), `"approved"`) {
		t.Fatalf("audit log missing state: %s", data)
	}
}

func TestRuntimeAuditLogLoadsEntries(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	runtime := NewRuntime("", WithAuditLogPath(logPath), WithCommandAllowlist([]string{"make test"}))
	_, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "make test", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	// Create a new runtime that loads from the same log
	runtime2 := NewRuntime("", WithAuditLogPath(logPath))
	runtime2.LoadAuditLog()
	approvals := runtime2.ListCommandApprovals(context.Background())
	if len(approvals) == 0 {
		t.Fatalf("no approvals loaded from audit log")
	}
	if approvals[0].Command != "make test" {
		t.Fatalf("loaded approval command = %q, want %q", approvals[0].Command, "make test")
	}
}

func TestRuntimeAuditLogDisabledWhenPathEmpty(t *testing.T) {
	t.Setenv("LINEA_AUDIT_LOG_PATH", "")
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	_, err := runtime.AddCommandApproval(context.Background(), CommandApprovalInput{Command: "make test", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
}

func TestRuntimeCheckCommandCategoryAndDestructive(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"make test"}))
	check, err := runtime.CheckCommand(context.Background(), CommandCheckInput{Command: "make test"})
	if err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	if check.Category == "" {
		t.Fatalf("CheckCommand().Category is empty, want a category")
	}
	if check.Destructive {
		t.Fatalf("CheckCommand().Destructive = true, want false for 'make test'")
	}
}

func TestRuntimeStatusIncludesAutoApproveCategories(t *testing.T) {
	runtime := NewRuntime("", WithAutoApproveCategories([]string{"read", "write"}))
	status := runtime.Status(context.Background())
	if status.Unrestricted {
		t.Fatalf("Status().Unrestricted = true, want false")
	}
	if status.RunSummary.CommandApprovals != 0 {
		t.Fatalf("Status().RunSummary.CommandApprovals = %d, want 0", status.RunSummary.CommandApprovals)
	}
	if status.RunSummary.CommandChecks != 0 {
		t.Fatalf("Status().RunSummary.CommandChecks = %d, want 0", status.RunSummary.CommandChecks)
	}
	if status.RunSummary.CommandRuns != 0 {
		t.Fatalf("Status().RunSummary.CommandRuns = %d, want 0", status.RunSummary.CommandRuns)
	}
}

func TestRuntimeStartBackgroundJob(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
	job, err := runtime.StartBackgroundJob(context.Background(), BackgroundJobInput{Goal: "test background job"})
	if err != nil {
		t.Fatalf("StartBackgroundJob() error = %v", err)
	}
	if job.ID == "" {
		t.Fatalf("job.ID is empty")
	}
	if job.Goal != "test background job" {
		t.Fatalf("job.Goal = %q, want %q", job.Goal, "test background job")
	}
	if job.State != "running" {
		t.Fatalf("job.State = %q, want %q", job.State, "running")
	}
	if job.LoopID == "" {
		t.Fatalf("job.LoopID is empty")
	}
}

func TestRuntimeStartBackgroundJobDefaultsToAutoMode(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
	job, err := runtime.StartBackgroundJob(context.Background(), BackgroundJobInput{Goal: "test", Mode: "invalid"})
	if err != nil {
		t.Fatalf("StartBackgroundJob() error = %v", err)
	}
	if job.State != "running" {
		t.Fatalf("job.State = %q, want %q", job.State, "running")
	}
}

func TestRuntimeCancelBackgroundJob(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
	job, err := runtime.StartBackgroundJob(context.Background(), BackgroundJobInput{Goal: "test"})
	if err != nil {
		t.Fatalf("StartBackgroundJob() error = %v", err)
	}
	cancelled, err := runtime.CancelBackgroundJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("CancelBackgroundJob() error = %v", err)
	}
	if cancelled.State != "cancelled" {
		t.Fatalf("cancelled.State = %q, want %q", cancelled.State, "cancelled")
	}
	if cancelled.ID != job.ID {
		t.Fatalf("cancelled.ID = %q, want %q", cancelled.ID, job.ID)
	}
}

func TestRuntimeCancelBackgroundJobNotFound(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
	_, err := runtime.CancelBackgroundJob(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("CancelBackgroundJob() error = nil, want error")
	}
}

func TestRuntimeCancelBackgroundJobNotRunning(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
	job, err := runtime.StartBackgroundJob(context.Background(), BackgroundJobInput{Goal: "test"})
	if err != nil {
		t.Fatalf("StartBackgroundJob() error = %v", err)
	}
	_, err = runtime.CancelBackgroundJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("first CancelBackgroundJob() error = %v", err)
	}
	_, err = runtime.CancelBackgroundJob(context.Background(), job.ID)
	if err == nil {
		t.Fatal("second CancelBackgroundJob() error = nil, want error")
	}
}

func TestRuntimeBackgroundJobsInStatus(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()))
	job, err := runtime.StartBackgroundJob(context.Background(), BackgroundJobInput{Goal: "test"})
	if err != nil {
		t.Fatalf("StartBackgroundJob() error = %v", err)
	}
	status := runtime.Status(context.Background())
	if len(status.BackgroundJobs) != 1 || status.BackgroundJobs[0].ID != job.ID {
		t.Fatalf("Status().BackgroundJobs = %#v, want 1 job with ID %q", status.BackgroundJobs, job.ID)
	}
}

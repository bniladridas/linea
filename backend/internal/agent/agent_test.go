package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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

	execution, err := runtime.RunHook(context.Background(), "after_check", HookExecutionInput{Command: "printf ok"})
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
      "env": {"TOKEN": "secret"}
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
}

func TestStatusReportsUnavailableMCPConfig(t *testing.T) {
	runtime := NewRuntime("", WithMCPConfigPath(filepath.Join(t.TempDir(), "missing.json")))

	status := runtime.Status(context.Background())

	if len(status.MCPServers) != 1 || status.MCPServers[0].State != "unavailable" {
		t.Fatalf("mcp servers = %#v", status.MCPServers)
	}
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

	execution, err := runtime.RunSkill(context.Background(), "review_change", SkillExecutionInput{})
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

	run, err := runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok"})
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

func TestRuntimeRejectsCommandRunOutsideAllowlist(t *testing.T) {
	runtime := NewRuntime("", WithWorkspaceRoot(t.TempDir()), WithCommandAllowlist([]string{"printf ok"}))

	_, err := runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf no"})
	if err == nil {
		t.Fatal("RunCommand() error = nil, want error")
	}
}

func TestRuntimeRequiresWorkspaceForCommandRun(t *testing.T) {
	runtime := NewRuntime("", WithCommandAllowlist([]string{"printf ok"}))

	_, err := runtime.RunCommand(context.Background(), CommandCheckInput{Command: "printf ok"})
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

func TestWorkspaceDisabledByDefault(t *testing.T) {
	runtime := NewRuntime("")
	if runtime.WorkspaceEnabled() {
		t.Fatal("WorkspaceEnabled() = true, want false")
	}
	if _, err := runtime.SearchFiles(context.Background(), "agent"); !errors.Is(err, ErrWorkspaceDisabled) {
		t.Fatalf("SearchFiles() error = %v, want ErrWorkspaceDisabled", err)
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

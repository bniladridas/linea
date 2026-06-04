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

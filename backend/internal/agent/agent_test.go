package agent

import (
	"context"
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

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

package config

import "testing"

func TestEnvBoolAcceptsTrimmedMixedCaseValues(t *testing.T) {
	t.Setenv("LINEA_TEST_BOOL", " True ")
	if !envBool("LINEA_TEST_BOOL", false) {
		t.Fatal("envBool(True) = false, want true")
	}

	t.Setenv("LINEA_TEST_BOOL", " Off ")
	if envBool("LINEA_TEST_BOOL", true) {
		t.Fatal("envBool(Off) = true, want false")
	}
}

func TestEnvBoolUsesFallbackForUnknownValues(t *testing.T) {
	t.Setenv("LINEA_TEST_BOOL", "maybe")
	if !envBool("LINEA_TEST_BOOL", true) {
		t.Fatal("envBool(unknown) = false, want fallback true")
	}
}

func TestLoadReadsAgentRulesPath(t *testing.T) {
	t.Setenv("LINEA_RULES_FILE", "docs/rules.md")
	t.Setenv("LINEA_WORKSPACE_DIR", "/tmp/linea")

	cfg := Load()

	if cfg.AgentRulesPath != "docs/rules.md" {
		t.Fatalf("AgentRulesPath = %q, want docs/rules.md", cfg.AgentRulesPath)
	}
	if cfg.AgentWorkspaceDir != "/tmp/linea" {
		t.Fatalf("AgentWorkspaceDir = %q, want /tmp/linea", cfg.AgentWorkspaceDir)
	}
}

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
	t.Setenv("LINEA_SKILLS_DIR", "docs/skills")
	t.Setenv("LINEA_WORKSPACE_DIR", "/tmp/linea")
	t.Setenv("LINEA_LSP_COMMAND", "gopls")
	t.Setenv("LINEA_MCP_CONFIG", "mcp.json")
	t.Setenv("LINEA_COMMAND_ALLOWLIST", "make test, go test ./... ")

	cfg := Load()

	if cfg.AgentRulesPath != "docs/rules.md" {
		t.Fatalf("AgentRulesPath = %q, want docs/rules.md", cfg.AgentRulesPath)
	}
	if cfg.AgentSkillsDir != "docs/skills" {
		t.Fatalf("AgentSkillsDir = %q, want docs/skills", cfg.AgentSkillsDir)
	}
	if cfg.AgentWorkspaceDir != "/tmp/linea" {
		t.Fatalf("AgentWorkspaceDir = %q, want /tmp/linea", cfg.AgentWorkspaceDir)
	}
	if cfg.AgentLSPCommand != "gopls" {
		t.Fatalf("AgentLSPCommand = %q, want gopls", cfg.AgentLSPCommand)
	}
	if cfg.AgentMCPConfig != "mcp.json" {
		t.Fatalf("AgentMCPConfig = %q, want mcp.json", cfg.AgentMCPConfig)
	}
	if len(cfg.AgentCommandAllowlist) != 2 || cfg.AgentCommandAllowlist[0] != "make test" || cfg.AgentCommandAllowlist[1] != "go test ./..." {
		t.Fatalf("AgentCommandAllowlist = %#v", cfg.AgentCommandAllowlist)
	}
}

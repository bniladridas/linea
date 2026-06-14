package config

import (
	"testing"
)

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

func TestEnvBoolTrimsSpaces(t *testing.T) {
	t.Setenv("LINEA_TEST_BOOL", " 1 ")
	if !envBool("LINEA_TEST_BOOL", false) {
		t.Fatal("envBool(' 1 ') = false, want true")
	}
}

func TestEnvBoolAcceptsYesAndOn(t *testing.T) {
	for _, val := range []string{"yes", "YES", "on", "ON"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("LINEA_TEST_BOOL", val)
			if !envBool("LINEA_TEST_BOOL", false) {
				t.Fatalf("envBool(%q) = false, want true", val)
			}
		})
	}
}

func TestEnvBoolAcceptsNoAndOff(t *testing.T) {
	for _, val := range []string{"no", "NO", "off", "OFF"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("LINEA_TEST_BOOL", val)
			if envBool("LINEA_TEST_BOOL", true) {
				t.Fatalf("envBool(%q) = true, want false", val)
			}
		})
	}
}

func TestEnvBoolUsesFallbackForEmptyString(t *testing.T) {
	t.Setenv("LINEA_TEST_BOOL", "")
	if !envBool("LINEA_TEST_BOOL", true) {
		t.Fatal("envBool('') = false, want fallback true")
	}
}

func TestEnvReturnsValueWhenSet(t *testing.T) {
	t.Setenv("LINEA_TEST_ENV", "set-value")
	if got := env("LINEA_TEST_ENV", "fallback"); got != "set-value" {
		t.Fatalf("env() = %q, want set-value", got)
	}
}

func TestEnvReturnsFallbackWhenNotSet(t *testing.T) {
	if got := env("LINEA_TEST_ENV_NONEXISTENT", "fallback"); got != "fallback" {
		t.Fatalf("env() = %q, want fallback", got)
	}
}

func TestEnvReturnsFallbackForEmptyValue(t *testing.T) {
	t.Setenv("LINEA_TEST_ENV_EMPTY", "")
	if got := env("LINEA_TEST_ENV_EMPTY", "fallback"); got != "fallback" {
		t.Fatalf("env() = %q, want fallback", got)
	}
}

func TestEnvListReturnsNilForEmptyEnv(t *testing.T) {
	t.Setenv("LINEA_TEST_LIST", "")
	if got := envList("LINEA_TEST_LIST"); got != nil {
		t.Fatalf("envList() = %#v, want nil", got)
	}
}

func TestEnvListSplitsByComma(t *testing.T) {
	t.Setenv("LINEA_TEST_LIST", "a, b, c")
	got := envList("LINEA_TEST_LIST")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("envList() = %#v", got)
	}
}

func TestEnvListSkipsEmptyParts(t *testing.T) {
	t.Setenv("LINEA_TEST_LIST", "a,,b,")
	got := envList("LINEA_TEST_LIST")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("envList() = %#v", got)
	}
}

func TestEnvListReturnsSingleItem(t *testing.T) {
	t.Setenv("LINEA_TEST_LIST", "single")
	got := envList("LINEA_TEST_LIST")
	if len(got) != 1 || got[0] != "single" {
		t.Fatalf("envList() = %#v", got)
	}
}

func TestLoadDefaultsAPIAddrToLoopback(t *testing.T) {
	t.Setenv("API_ADDR", "")

	cfg := Load()

	if cfg.APIAddr != "127.0.0.1:8080" {
		t.Fatalf("APIAddr = %q, want 127.0.0.1:8080", cfg.APIAddr)
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg := Load()

	if cfg.APIAddr != "127.0.0.1:8080" {
		t.Errorf("APIAddr = %q", cfg.APIAddr)
	}
	if cfg.GeminiModel != "gemini-2.5-flash-lite" {
		t.Errorf("GeminiModel = %q", cfg.GeminiModel)
	}
	if cfg.SambaNovaBaseURL != "https://api.sambanova.ai/v1" {
		t.Errorf("SambaNovaBaseURL = %q", cfg.SambaNovaBaseURL)
	}
	if !cfg.SambaNovaEnabled {
		t.Error("SambaNovaEnabled = false")
	}
	if cfg.CerebrasBaseURL != "https://api.cerebras.ai/v1" {
		t.Errorf("CerebrasBaseURL = %q", cfg.CerebrasBaseURL)
	}
	if !cfg.CerebrasEnabled {
		t.Error("CerebrasEnabled = false")
	}
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("OllamaBaseURL = %q", cfg.OllamaBaseURL)
	}
	if cfg.OllamaModel != "qwen2.5-coder:1.5b" {
		t.Errorf("OllamaModel = %q", cfg.OllamaModel)
	}
	if !cfg.OllamaFallback {
		t.Error("OllamaFallback = false")
	}
	if cfg.WebOrigin != "http://localhost:5173" {
		t.Errorf("WebOrigin = %q", cfg.WebOrigin)
	}
}

func TestLoadReadsAllEnvs(t *testing.T) {
	t.Setenv("API_ADDR", ":9090")
	t.Setenv("LINEA_RULES_FILE", "docs/rules.md")
	t.Setenv("LINEA_SKILLS_DIR", "docs/skills")
	t.Setenv("LINEA_WORKSPACE_DIR", "/tmp/linea")
	t.Setenv("LINEA_AGENT_DEVELOPER_MODE", "1")
	t.Setenv("LINEA_AGENT_WORKSPACE_TRUST", "full")
	t.Setenv("LINEA_LSP_COMMAND", "gopls")
	t.Setenv("LINEA_MCP_CONFIG", "mcp.json")
	t.Setenv("LINEA_COMMAND_ALLOWLIST", "make test, go test ./... ")
	t.Setenv("DATABASE_URL", "postgres://localhost/linea")
	t.Setenv("GEMINI_API_KEY", "gemini-key")
	t.Setenv("GEMINI_MODEL", "gemini-2.0-pro")
	t.Setenv("SAMBANOVA_API_KEY", "samba-key")
	t.Setenv("SAMBANOVA_BASE_URL", "https://sambanova.example.com")
	t.Setenv("SAMBANOVA_ENABLED", "false")
	t.Setenv("SAMBANOVA_MODEL", "custom-model")
	t.Setenv("CEREBRAS_API_KEY", "cerebras-key")
	t.Setenv("CEREBRAS_BASE_URL", "https://cerebras.example.com")
	t.Setenv("CEREBRAS_ENABLED", "false")
	t.Setenv("CEREBRAS_MODEL", "custom-cerebras")
	t.Setenv("BRAVE_SEARCH_API_KEY", "brave")
	t.Setenv("SEARXNG_URL", "http://127.0.0.1:8888")
	t.Setenv("OLLAMA_BASE_URL", "http://ollama:11434")
	t.Setenv("OLLAMA_MODEL", "llama3")
	t.Setenv("OLLAMA_FALLBACK", "false")
	t.Setenv("STATIC_DIR", "./static")
	t.Setenv("WEB_ORIGIN", "https://linea.example.com")
	t.Setenv("LINEA_SYNC_URL", "https://sync.example.com")
	t.Setenv("LINEA_SYNC_TOKEN", "sync-token")

	cfg := Load()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"APIAddr", cfg.APIAddr, ":9090"},
		{"AgentRulesPath", cfg.AgentRulesPath, "docs/rules.md"},
		{"AgentSkillsDir", cfg.AgentSkillsDir, "docs/skills"},
		{"AgentWorkspaceDir", cfg.AgentWorkspaceDir, "/tmp/linea"},
		{"AgentDeveloperMode", cfg.AgentDeveloperMode, true},
		{"AgentWorkspaceTrust", cfg.AgentWorkspaceTrust, "full"},
		{"AgentLSPCommand", cfg.AgentLSPCommand, "gopls"},
		{"AgentMCPConfig", cfg.AgentMCPConfig, "mcp.json"},
		{"DatabaseURL", cfg.DatabaseURL, "postgres://localhost/linea"},
		{"GeminiAPIKey", cfg.GeminiAPIKey, "gemini-key"},
		{"GeminiModel", cfg.GeminiModel, "gemini-2.0-pro"},
		{"SambaNovaAPIKey", cfg.SambaNovaAPIKey, "samba-key"},
		{"SambaNovaBaseURL", cfg.SambaNovaBaseURL, "https://sambanova.example.com"},
		{"SambaNovaEnabled", cfg.SambaNovaEnabled, false},
		{"SambaNovaModel", cfg.SambaNovaModel, "custom-model"},
		{"CerebrasAPIKey", cfg.CerebrasAPIKey, "cerebras-key"},
		{"CerebrasBaseURL", cfg.CerebrasBaseURL, "https://cerebras.example.com"},
		{"CerebrasEnabled", cfg.CerebrasEnabled, false},
		{"CerebrasModel", cfg.CerebrasModel, "custom-cerebras"},
		{"BraveSearchAPIKey", cfg.BraveSearchAPIKey, "brave"},
		{"SearXNGURL", cfg.SearXNGURL, "http://127.0.0.1:8888"},
		{"OllamaBaseURL", cfg.OllamaBaseURL, "http://ollama:11434"},
		{"OllamaModel", cfg.OllamaModel, "llama3"},
		{"OllamaFallback", cfg.OllamaFallback, false},
		{"StaticDir", cfg.StaticDir, "./static"},
		{"WebOrigin", cfg.WebOrigin, "https://linea.example.com"},
		{"SyncURL", cfg.SyncURL, "https://sync.example.com"},
		{"SyncToken", cfg.SyncToken, "sync-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoadCommandAllowlistLength(t *testing.T) {
	t.Setenv("LINEA_COMMAND_ALLOWLIST", "make test, go test ./... ")
	cfg := Load()
	if len(cfg.AgentCommandAllowlist) != 2 || cfg.AgentCommandAllowlist[0] != "make test" || cfg.AgentCommandAllowlist[1] != "go test ./..." {
		t.Fatalf("AgentCommandAllowlist = %#v", cfg.AgentCommandAllowlist)
	}
}




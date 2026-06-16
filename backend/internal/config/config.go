package config

import (
	"os"
	"strings"
)

type Config struct {
	APIAddr                 string
	AgentRulesPath          string
	AgentDeveloperMode      bool
	AgentWorkspaceTrust     string
	AgentSkillsDir          string
	AgentWorkspaceDir       string
	AgentLSPCommand         string
	AgentMCPConfig          string
	AgentCommandAllowlist   []string
	DatabaseURL             string
	GeminiAPIKey            string
	GeminiModel             string
	SambaNovaAPIKey         string
	SambaNovaBaseURL        string
	SambaNovaEnabled        bool
	SambaNovaModel          string
	CerebrasAPIKey          string
	CerebrasBaseURL         string
	CerebrasEnabled         bool
	CerebrasModel           string
	BraveSearchAPIKey       string
	SearXNGURL              string
	VLLMBaseURL             string
	VLLMModel               string
	VLLMEnabled             bool
	MLXBaseURL              string
	MLXModel                string
	MLXEnabled              bool
	OpenAICompatibleBaseURL string
	OpenAICompatibleAPIKey  string
	OpenAICompatibleModel   string
	OpenAICompatibleEnabled bool
	LineaSaasMode           bool
	LineaSaasAdminKey       string
	OllamaBaseURL           string
	OllamaModel             string
	OllamaFallback          bool
	StaticDir               string
	WebOrigin               string
	SyncURL                 string
	SyncToken               string
	OAuthEncryptionKey      string
	GitHubClientID          string
	GitHubClientSecret      string
	GitLabClientID          string
	GitLabClientSecret      string
	GoogleClientID          string
	GoogleClientSecret      string
	EnableAPI               bool
	APIKey                  string
	EnableAccounts          bool
	EnableSync              bool
}

func Load() Config {
	return Config{
		APIAddr:                 env("API_ADDR", "127.0.0.1:8080"),
		AgentRulesPath:          env("LINEA_RULES_FILE", "AGENTS.md"),
		AgentDeveloperMode:      envBool("LINEA_AGENT_DEVELOPER_MODE", false),
		AgentWorkspaceTrust:     os.Getenv("LINEA_AGENT_WORKSPACE_TRUST"),
		AgentSkillsDir:          os.Getenv("LINEA_SKILLS_DIR"),
		AgentWorkspaceDir:       os.Getenv("LINEA_WORKSPACE_DIR"),
		AgentLSPCommand:         os.Getenv("LINEA_LSP_COMMAND"),
		AgentMCPConfig:          os.Getenv("LINEA_MCP_CONFIG"),
		AgentCommandAllowlist:   envList("LINEA_COMMAND_ALLOWLIST"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		GeminiAPIKey:            os.Getenv("GEMINI_API_KEY"),
		GeminiModel:             env("GEMINI_MODEL", "gemini-2.5-flash-lite"),
		SambaNovaAPIKey:         os.Getenv("SAMBANOVA_API_KEY"),
		SambaNovaBaseURL:        env("SAMBANOVA_BASE_URL", "https://api.sambanova.ai/v1"),
		SambaNovaEnabled:        envBool("SAMBANOVA_ENABLED", true),
		SambaNovaModel:          env("SAMBANOVA_MODEL", "gpt-oss-120b"),
		CerebrasAPIKey:          os.Getenv("CEREBRAS_API_KEY"),
		CerebrasBaseURL:         env("CEREBRAS_BASE_URL", "https://api.cerebras.ai/v1"),
		CerebrasEnabled:         envBool("CEREBRAS_ENABLED", true),
		CerebrasModel:           env("CEREBRAS_MODEL", "gpt-oss-120b"),
		BraveSearchAPIKey:       os.Getenv("BRAVE_SEARCH_API_KEY"),
		SearXNGURL:              os.Getenv("SEARXNG_URL"),
		VLLMBaseURL:             env("VLLM_BASE_URL", "http://localhost:8000/v1"),
		VLLMModel:               env("VLLM_MODEL", "Qwen/Qwen2.5-Coder-1.5B-Instruct"),
		VLLMEnabled:             envBool("VLLM_ENABLED", false),
		MLXBaseURL:              env("MLX_BASE_URL", "http://localhost:8080/v1"),
		MLXModel:                env("MLX_MODEL", "mlx-community/Qwen2.5-Coder-1.5B-Instruct-4bit"),
		MLXEnabled:              envBool("MLX_ENABLED", false),
		OpenAICompatibleBaseURL: os.Getenv("OPENAI_COMPATIBLE_BASE_URL"),
		OpenAICompatibleAPIKey:  os.Getenv("OPENAI_COMPATIBLE_API_KEY"),
		OpenAICompatibleModel:   os.Getenv("OPENAI_COMPATIBLE_MODEL"),
		OpenAICompatibleEnabled: envBool("OPENAI_COMPATIBLE_ENABLED", false),
		OllamaBaseURL:           env("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaModel:             env("OLLAMA_MODEL", "qwen2.5-coder:1.5b"),
		OllamaFallback:          envBool("OLLAMA_FALLBACK", true),
		StaticDir:               os.Getenv("STATIC_DIR"),
		WebOrigin:               env("WEB_ORIGIN", "http://localhost:5173"),
		SyncURL:                 os.Getenv("LINEA_SYNC_URL"),
		SyncToken:               os.Getenv("LINEA_SYNC_TOKEN"),
		OAuthEncryptionKey:      os.Getenv("LINEA_OAUTH_ENCRYPTION_KEY"),
		GitHubClientID:          os.Getenv("LINEA_GITHUB_CLIENT_ID"),
		GitHubClientSecret:      os.Getenv("LINEA_GITHUB_CLIENT_SECRET"),
		GitLabClientID:          os.Getenv("LINEA_GITLAB_CLIENT_ID"),
		GitLabClientSecret:      os.Getenv("LINEA_GITLAB_CLIENT_SECRET"),
		GoogleClientID:          os.Getenv("LINEA_GOOGLE_CLIENT_ID"),
		GoogleClientSecret:      os.Getenv("LINEA_GOOGLE_CLIENT_SECRET"),
		LineaSaasMode:           envBool("LINEA_SAAS_MODE", false),
		LineaSaasAdminKey:       os.Getenv("LINEA_SAAS_ADMIN_KEY"),
		EnableAPI:               envBool("LINEA_ENABLE_API", false),
		APIKey:                  os.Getenv("LINEA_API_KEY"),
		EnableAccounts:          envBool("LINEA_ENABLE_ACCOUNTS", false),
		EnableSync:              envBool("LINEA_ENABLE_SYNC", false),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

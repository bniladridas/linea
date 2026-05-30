package config

import "os"

type Config struct {
	APIAddr          string
	DatabaseURL      string
	GeminiAPIKey     string
	GeminiModel      string
	SambaNovaAPIKey  string
	SambaNovaBaseURL string
	SambaNovaEnabled bool
	SambaNovaModel   string
	CerebrasAPIKey   string
	CerebrasBaseURL  string
	CerebrasEnabled  bool
	CerebrasModel    string
	OllamaBaseURL    string
	OllamaModel      string
	OllamaFallback   bool
	StaticDir        string
	WebOrigin        string
}

func Load() Config {
	return Config{
		APIAddr:          env("API_ADDR", ":8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		GeminiAPIKey:     os.Getenv("GEMINI_API_KEY"),
		GeminiModel:      env("GEMINI_MODEL", "gemini-2.5-flash-lite"),
		SambaNovaAPIKey:  os.Getenv("SAMBANOVA_API_KEY"),
		SambaNovaBaseURL: env("SAMBANOVA_BASE_URL", "https://api.sambanova.ai/v1"),
		SambaNovaEnabled: envBool("SAMBANOVA_ENABLED", true),
		SambaNovaModel:   env("SAMBANOVA_MODEL", "gpt-oss-120b"),
		CerebrasAPIKey:   os.Getenv("CEREBRAS_API_KEY"),
		CerebrasBaseURL:  env("CEREBRAS_BASE_URL", "https://api.cerebras.ai/v1"),
		CerebrasEnabled:  envBool("CEREBRAS_ENABLED", true),
		CerebrasModel:    env("CEREBRAS_MODEL", "gpt-oss-120b"),
		OllamaBaseURL:    env("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaModel:      env("OLLAMA_MODEL", "qwen2.5-coder:1.5b"),
		OllamaFallback:   envBool("OLLAMA_FALLBACK", true),
		StaticDir:        os.Getenv("STATIC_DIR"),
		WebOrigin:        env("WEB_ORIGIN", "http://localhost:5173"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}

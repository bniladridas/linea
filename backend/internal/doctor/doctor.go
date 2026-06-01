package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"linea/backend/internal/config"
	"linea/backend/internal/llm"
	"linea/backend/internal/migrate"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
	"linea/backend/internal/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Result struct {
	Name    string
	Status  Status
	Message string
}

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

func Run(ctx context.Context, cfg config.Config) []Result {
	var results []Result
	results = append(results, checkConfig(cfg)...)
	results = append(results, checkConfigFile())
	results = append(results, checkEmbeddedWeb())
	results = append(results, checkDatabase(ctx, cfg.DatabaseURL)...)
	results = append(results, checkMigrations(ctx, cfg.DatabaseURL)...)
	results = append(results, checkSearch(ctx))
	ollamaResult := checkOllama(ctx, cfg)
	results = append(results, ollamaResult)
	hasFallback := (cfg.SambaNovaEnabled && cfg.SambaNovaAPIKey != "") ||
		(cfg.CerebrasEnabled && cfg.CerebrasAPIKey != "") ||
		(ollamaResult.Status == Pass && cfg.OllamaFallback)
	results = append(results, checkGemini(ctx, cfg, hasFallback))
	if cfg.SambaNovaEnabled {
		results = append(results, checkOpenAICompatibleGeneration(ctx, "sambanova generation", cfg.SambaNovaAPIKey, cfg.SambaNovaBaseURL, cfg.SambaNovaModel))
	} else {
		results = append(results, Result{Name: "sambanova generation", Status: Warn, Message: "disabled"})
	}
	if cfg.CerebrasEnabled {
		results = append(results, checkOpenAICompatibleGeneration(ctx, "cerebras generation", cfg.CerebrasAPIKey, cfg.CerebrasBaseURL, cfg.CerebrasModel))
	} else {
		results = append(results, Result{Name: "cerebras generation", Status: Warn, Message: "disabled"})
	}
	if cfg.OllamaFallback {
		results = append(results, checkOllamaGeneration(ctx, cfg, ollamaResult.Status == Pass))
	} else {
		results = append(results, Result{Name: "ollama generation", Status: Warn, Message: "disabled"})
	}
	return results
}

func checkConfigFile() Result {
	if _, err := os.Stat(".env"); err == nil {
		return Result{Name: "config file", Status: Pass, Message: ".env"}
	}
	path := config.DefaultEnvFilePath()
	if path == "" {
		return Result{Name: "config file", Status: Warn, Message: "using shell variables and defaults"}
	}
	_, err := os.Stat(path)
	if err == nil {
		return Result{Name: "config file", Status: Pass, Message: path}
	}
	if os.IsNotExist(err) {
		return Result{Name: "config file", Status: Warn, Message: path + " not found; using shell variables and defaults"}
	}
	return Result{Name: "config file", Status: Warn, Message: err.Error()}
}

func checkMigrations(ctx context.Context, databaseURL string) []Result {
	if databaseURL == "" {
		return nil
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return []Result{{Name: "database migrations", Status: Fail, Message: err.Error()}}
	}
	defer pool.Close()

	names, err := migrate.Names()
	if err != nil {
		return []Result{{Name: "database migrations", Status: Fail, Message: err.Error()}}
	}
	for _, name := range names {
		var exists bool
		err := pool.QueryRow(ctx, `select exists(select 1 from schema_migrations where name = $1)`, name).Scan(&exists)
		if err != nil {
			return []Result{{Name: "database migrations", Status: Fail, Message: "run linea -migrate"}}
		}
		if !exists {
			return []Result{{Name: "database migrations", Status: Fail, Message: name + " is pending; run linea -migrate"}}
		}
	}
	return []Result{{Name: "database migrations", Status: Pass, Message: "up to date"}}
}

func HasFailure(results []Result) bool {
	for _, result := range results {
		if result.Status == Fail {
			return true
		}
	}
	return false
}

func Print(results []Result) {
	for _, result := range results {
		fmt.Printf("%-4s %s", result.Status, result.Name)
		if result.Message != "" {
			fmt.Printf(" - %s", result.Message)
		}
		fmt.Println()
	}
}

func checkConfig(cfg config.Config) []Result {
	results := []Result{{
		Name:    "api address",
		Status:  Pass,
		Message: cfg.APIAddr,
	}}
	if cfg.GeminiAPIKey == "" {
		results = append(results, Result{Name: "gemini api key", Status: Fail, Message: "GEMINI_API_KEY is not set"})
	} else {
		results = append(results, Result{Name: "gemini api key", Status: Pass, Message: "configured"})
	}
	if cfg.GeminiModel == "" {
		results = append(results, Result{Name: "gemini model", Status: Fail, Message: "GEMINI_MODEL is empty"})
	} else {
		results = append(results, Result{Name: "gemini model", Status: Pass, Message: cfg.GeminiModel})
	}
	results = append(results, checkOpenAICompatibleConfig("sambanova", cfg.SambaNovaEnabled, cfg.SambaNovaAPIKey, cfg.SambaNovaBaseURL, cfg.SambaNovaModel))
	results = append(results, checkOpenAICompatibleConfig("cerebras", cfg.CerebrasEnabled, cfg.CerebrasAPIKey, cfg.CerebrasBaseURL, cfg.CerebrasModel))
	if cfg.OllamaFallback {
		results = append(results, Result{Name: "ollama fallback", Status: Pass, Message: cfg.OllamaModel + " at " + cfg.OllamaBaseURL})
	} else {
		results = append(results, Result{Name: "ollama fallback", Status: Warn, Message: "disabled"})
	}
	return results
}

func checkOpenAICompatibleConfig(name string, enabled bool, apiKey, baseURL, model string) Result {
	if !enabled {
		return Result{Name: name + " fallback", Status: Warn, Message: "disabled"}
	}
	if apiKey == "" {
		return Result{Name: name + " fallback", Status: Warn, Message: "API key not set"}
	}
	if baseURL == "" {
		return Result{Name: name + " fallback", Status: Fail, Message: "base URL is empty"}
	}
	if model == "" {
		return Result{Name: name + " fallback", Status: Fail, Message: "model is empty"}
	}
	return Result{Name: name + " fallback", Status: Pass, Message: model + " at " + baseURL}
}

func checkEmbeddedWeb() Result {
	file, err := web.Files().Open("index.html")
	if err != nil {
		return Result{Name: "embedded web ui", Status: Fail, Message: err.Error()}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Result{Name: "embedded web ui", Status: Fail, Message: err.Error()}
	}
	if info.Size() == 0 {
		return Result{Name: "embedded web ui", Status: Fail, Message: "index.html is empty"}
	}
	return Result{Name: "embedded web ui", Status: Pass, Message: "available"}
}

func checkDatabase(ctx context.Context, databaseURL string) []Result {
	if databaseURL == "" {
		return []Result{{Name: "database", Status: Warn, Message: "DATABASE_URL is not set; server will use in-memory storage"}}
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return []Result{{Name: "database", Status: Fail, Message: err.Error()}}
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return []Result{{Name: "database", Status: Fail, Message: err.Error()}}
	}

	results := []Result{{Name: "database", Status: Pass, Message: "connected"}}
	for _, table := range []string{"conversations", "messages"} {
		var exists bool
		err := pool.QueryRow(ctx, `select exists (
			select 1
			from information_schema.tables
			where table_schema = 'public' and table_name = $1
		)`, table).Scan(&exists)
		if err != nil {
			results = append(results, Result{Name: "database schema", Status: Fail, Message: err.Error()})
			continue
		}
		if !exists {
			results = append(results, Result{Name: "database schema", Status: Fail, Message: table + " table is missing"})
			continue
		}
		results = append(results, Result{Name: "database schema", Status: Pass, Message: table + " table exists"})
	}
	return results
}

func checkSearch(ctx context.Context) Result {
	results, err := search.NewClient().Search(ctx, "OpenAI")
	if err != nil {
		return Result{Name: "web search", Status: Fail, Message: err.Error()}
	}
	if len(results) == 0 {
		return Result{Name: "web search", Status: Warn, Message: "provider responded with no instant-answer results"}
	}
	return Result{Name: "web search", Status: Pass, Message: "provider reachable"}
}

func checkOllama(ctx context.Context, cfg config.Config) Result {
	if !cfg.OllamaFallback {
		return Result{Name: "ollama local model", Status: Warn, Message: "fallback disabled"}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.OllamaBaseURL, "/")+"/api/tags", nil)
	if err != nil {
		return Result{Name: "ollama local model", Status: Fail, Message: err.Error()}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{Name: "ollama local model", Status: Warn, Message: err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Result{Name: "ollama local model", Status: Warn, Message: res.Status}
	}

	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil {
		return Result{Name: "ollama local model", Status: Fail, Message: err.Error()}
	}
	for _, model := range payload.Models {
		if model.Name == cfg.OllamaModel {
			return Result{Name: "ollama local model", Status: Pass, Message: cfg.OllamaModel + " available"}
		}
	}
	return Result{Name: "ollama local model", Status: Warn, Message: cfg.OllamaModel + " is not installed"}
}

func checkGemini(ctx context.Context, cfg config.Config, hasFallback bool) Result {
	if cfg.GeminiAPIKey == "" {
		if hasFallback {
			return Result{Name: "gemini generation", Status: Warn, Message: "missing API key; fallback available"}
		}
		return Result{Name: "gemini generation", Status: Fail, Message: "missing API key and fallback unavailable"}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var reply strings.Builder
	err := llm.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel).GenerateStream(
		ctx,
		[]store.Message{{Role: "user", Content: "Reply with only the word ok."}},
		nil,
		nil,
		func(chunk string) error {
			reply.WriteString(chunk)
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, llm.ErrQuotaExceeded) && hasFallback {
			return Result{Name: "gemini generation", Status: Warn, Message: "quota exceeded; fallback available"}
		}
		return Result{Name: "gemini generation", Status: Fail, Message: redact(err.Error())}
	}
	if strings.TrimSpace(reply.String()) == "" {
		return Result{Name: "gemini generation", Status: Fail, Message: "empty response"}
	}
	return Result{Name: "gemini generation", Status: Pass, Message: "response received"}
}

func checkOpenAICompatibleGeneration(ctx context.Context, name, apiKey, baseURL, model string) Result {
	providerName := strings.TrimSuffix(name, " generation")
	if apiKey == "" {
		return Result{Name: name, Status: Warn, Message: "API key not set"}
	}
	if baseURL == "" {
		return Result{Name: name, Status: Fail, Message: "base URL is empty"}
	}
	if model == "" {
		return Result{Name: name, Status: Fail, Message: "model is empty"}
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var reply strings.Builder
	err := llm.NewOpenAICompatibleClient(providerName, baseURL, apiKey, model).GenerateStream(
		ctx,
		[]store.Message{{Role: "user", Content: "Reply with only the word ok."}},
		nil,
		nil,
		func(chunk string) error {
			reply.WriteString(chunk)
			return nil
		},
	)
	if err != nil {
		return Result{Name: name, Status: Warn, Message: "unavailable: " + redact(err.Error())}
	}
	if strings.TrimSpace(reply.String()) == "" {
		return Result{Name: name, Status: Warn, Message: "empty response"}
	}
	return Result{Name: name, Status: Pass, Message: "response received"}
}

func checkOllamaGeneration(ctx context.Context, cfg config.Config, modelAvailable bool) Result {
	if !modelAvailable {
		return Result{Name: "ollama generation", Status: Warn, Message: "local model unavailable"}
	}
	if cfg.OllamaBaseURL == "" {
		return Result{Name: "ollama generation", Status: Fail, Message: "base URL is empty"}
	}
	if cfg.OllamaModel == "" {
		return Result{Name: "ollama generation", Status: Fail, Message: "model is empty"}
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var reply strings.Builder
	err := llm.NewOllamaClient(cfg.OllamaBaseURL, cfg.OllamaModel).GenerateStream(
		ctx,
		[]store.Message{{Role: "user", Content: "Reply with only the word ok."}},
		nil,
		nil,
		func(chunk string) error {
			reply.WriteString(chunk)
			return nil
		},
	)
	if err != nil {
		return Result{Name: "ollama generation", Status: Warn, Message: redact(err.Error())}
	}
	if strings.TrimSpace(reply.String()) == "" {
		return Result{Name: "ollama generation", Status: Warn, Message: "empty response"}
	}
	return Result{Name: "ollama generation", Status: Pass, Message: "response received"}
}

func CheckServer(ctx context.Context, baseURL string) []Result {
	client := &http.Client{Timeout: 5 * time.Second}
	results := make([]Result, 0, 2)
	for _, endpoint := range []string{"/healthz", "/"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+endpoint, nil)
		if err != nil {
			results = append(results, Result{Name: "server " + endpoint, Status: Fail, Message: err.Error()})
			continue
		}
		res, err := client.Do(req)
		if err != nil {
			results = append(results, Result{Name: "server " + endpoint, Status: Fail, Message: err.Error()})
			continue
		}
		res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			results = append(results, Result{Name: "server " + endpoint, Status: Fail, Message: res.Status})
			continue
		}
		results = append(results, Result{Name: "server " + endpoint, Status: Pass, Message: res.Status})
	}
	return results
}

func redact(message string) string {
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 240 {
		return message[:237] + "..."
	}
	return message
}

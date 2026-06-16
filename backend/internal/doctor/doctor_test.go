package doctor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"linea/backend/internal/config"
)

func TestCheckOllamaWarnsWhenServerIsSleeping(t *testing.T) {
	result := checkOllama(context.Background(), config.Config{
		OllamaFallback: true,
		OllamaBaseURL:  "http://127.0.0.1:1",
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if result.Status != Warn {
		t.Fatalf("status = %s, want %s", result.Status, Warn)
	}
	if result.Message != "Ollama not running. Start with: ollama serve" {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestCheckOllamaWarnsWhenModelIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"other"}]}`))
	}))
	defer server.Close()

	result := checkOllama(context.Background(), config.Config{
		OllamaFallback: true,
		OllamaBaseURL:  server.URL,
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if result.Status != Warn {
		t.Fatalf("status = %s, want %s", result.Status, Warn)
	}
}

func TestCheckOllamaReturnsPassWhenModelFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:1.5b"}]}`))
	}))
	defer server.Close()

	result := checkOllama(context.Background(), config.Config{
		OllamaFallback: true,
		OllamaBaseURL:  server.URL,
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if result.Status != Pass {
		t.Fatalf("status = %s, want %s", result.Status, Pass)
	}
}

func TestCheckOllamaReturnsWarnWhenFallbackDisabled(t *testing.T) {
	result := checkOllama(context.Background(), config.Config{
		OllamaFallback: false,
	})
	if result.Status != Warn {
		t.Fatalf("status = %s, want %s", result.Status, Warn)
	}
}

func TestHasFailureReturnsTrueForFailures(t *testing.T) {
	results := []Result{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Fail},
	}
	if !HasFailure(results) {
		t.Fatal("HasFailure() = false, want true")
	}
}

func TestHasFailureReturnsFalseForPassAndWarn(t *testing.T) {
	results := []Result{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Warn},
	}
	if HasFailure(results) {
		t.Fatal("HasFailure() = true, want false")
	}
}

func TestHasFailureReturnsFalseForEmpty(t *testing.T) {
	if HasFailure(nil) {
		t.Fatal("HasFailure(nil) = true, want false")
	}
}

func TestCheckConfigIncludesAPIAddr(t *testing.T) {
	results := checkConfig(config.Config{
		APIAddr:            "127.0.0.1:8080",
		GeminiAPIKey:       "key",
		GeminiModel:        "gemini",
		SambaNovaEnabled:   false,
		CerebrasEnabled:    false,
		OllamaFallback:     false,
	})
	found := false
	for _, r := range results {
		if r.Name == "api address" && r.Message == "127.0.0.1:8080" {
			found = true
		}
	}
	if !found {
		t.Fatal("api address result not found")
	}
}

func TestCheckConfigFailsOnMissingGeminiKey(t *testing.T) {
	results := checkConfig(config.Config{
		GeminiAPIKey: "",
		GeminiModel:  "gemini",
	})
	found := false
	for _, r := range results {
		if r.Name == "gemini api key" && r.Status == Fail {
			found = true
		}
	}
	if !found {
		t.Fatal("missing gemini key not detected")
	}
}

func TestCheckConfigFailsOnEmptyModel(t *testing.T) {
	results := checkConfig(config.Config{
		GeminiAPIKey: "key",
		GeminiModel:  "",
	})
	found := false
	for _, r := range results {
		if r.Name == "gemini model" && r.Status == Fail {
			found = true
		}
	}
	if !found {
		t.Fatal("empty gemini model not detected")
	}
}

func TestCheckOpenAICompatibleConfigReturnsWarnWhenDisabled(t *testing.T) {
	result := checkOpenAICompatibleConfig("test", false, "", "", "", false)
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckOpenAICompatibleConfigReturnsWarnWhenMissingKey(t *testing.T) {
	result := checkOpenAICompatibleConfig("test", true, "", "http://base", "model", true)
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckOpenAICompatibleConfigReturnsFailWhenEmptyURL(t *testing.T) {
	result := checkOpenAICompatibleConfig("test", true, "key", "", "model", true)
	if result.Status != Fail {
		t.Fatalf("status = %s, want Fail", result.Status)
	}
}

func TestCheckOpenAICompatibleConfigReturnsFailWhenEmptyModel(t *testing.T) {
	result := checkOpenAICompatibleConfig("test", true, "key", "http://base", "", true)
	if result.Status != Fail {
		t.Fatalf("status = %s, want Fail", result.Status)
	}
}

func TestCheckOpenAICompatibleConfigReturnsPass(t *testing.T) {
	result := checkOpenAICompatibleConfig("test", true, "key", "http://base", "model", true)
	if result.Status != Pass {
		t.Fatalf("status = %s, want Pass", result.Status)
	}
}

func TestCheckOpenAICompatibleConfigSkipsKeyCheckWhenNotRequired(t *testing.T) {
	result := checkOpenAICompatibleConfig("test", true, "", "http://base", "model", false)
	if result.Status != Pass {
		t.Fatalf("status = %s, want Pass", result.Status)
	}
}

func TestCheckVLLMConfigReturnsWarnWhenDisabled(t *testing.T) {
	result := checkVLLMConfig(config.Config{VLLMEnabled: false})
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckVLLMConfigReturnsFailWhenEmptyURL(t *testing.T) {
	result := checkVLLMConfig(config.Config{VLLMEnabled: true, VLLMBaseURL: "", VLLMModel: "model"})
	if result.Status != Fail {
		t.Fatalf("status = %s, want Fail", result.Status)
	}
}

func TestCheckVLLMConfigReturnsFailWhenEmptyModel(t *testing.T) {
	result := checkVLLMConfig(config.Config{VLLMEnabled: true, VLLMBaseURL: "http://localhost:8000", VLLMModel: ""})
	if result.Status != Fail {
		t.Fatalf("status = %s, want Fail", result.Status)
	}
}

func TestCheckVLLMConfigReturnsPass(t *testing.T) {
	result := checkVLLMConfig(config.Config{VLLMEnabled: true, VLLMBaseURL: "http://localhost:8000", VLLMModel: "model"})
	if result.Status != Pass {
		t.Fatalf("status = %s, want Pass", result.Status)
	}
}

func TestCheckMLXConfigReturnsWarnWhenDisabled(t *testing.T) {
	result := checkMLXConfig(config.Config{MLXEnabled: false})
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckMLXConfigReturnsFailWhenEmptyURL(t *testing.T) {
	result := checkMLXConfig(config.Config{MLXEnabled: true, MLXBaseURL: "", MLXModel: "model"})
	if result.Status != Fail {
		t.Fatalf("status = %s, want Fail", result.Status)
	}
}

func TestCheckMLXConfigReturnsFailWhenEmptyModel(t *testing.T) {
	result := checkMLXConfig(config.Config{MLXEnabled: true, MLXBaseURL: "http://localhost:8080", MLXModel: ""})
	if result.Status != Fail {
		t.Fatalf("status = %s, want Fail", result.Status)
	}
}

func TestCheckMLXConfigReturnsPass(t *testing.T) {
	result := checkMLXConfig(config.Config{MLXEnabled: true, MLXBaseURL: "http://localhost:8080", MLXModel: "model"})
	if result.Status != Pass {
		t.Fatalf("status = %s, want Pass", result.Status)
	}
}

func TestCheckVLLMWarnsWhenServerUnreachable(t *testing.T) {
	result := checkVLLM(context.Background(), config.Config{
		VLLMEnabled: true,
		VLLMBaseURL: "http://127.0.0.1:1",
		VLLMModel:   "model",
	})
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckVLLMPassesWhenModelFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model"},{"id":"other"}]}`))
	}))
	defer server.Close()

	result := checkVLLM(context.Background(), config.Config{
		VLLMEnabled: true,
		VLLMBaseURL: server.URL,
		VLLMModel:   "model",
	})
	if result.Status != Pass {
		t.Fatalf("status = %s, want Pass", result.Status)
	}
}

func TestCheckVLLMWarnsWhenModelMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"other"}]}`))
	}))
	defer server.Close()

	result := checkVLLM(context.Background(), config.Config{
		VLLMEnabled: true,
		VLLMBaseURL: server.URL,
		VLLMModel:   "model",
	})
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckMLXWarnsWhenServerUnreachable(t *testing.T) {
	result := checkMLX(context.Background(), config.Config{
		MLXEnabled: true,
		MLXBaseURL: "http://127.0.0.1:1",
		MLXModel:   "model",
	})
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckMLXPassesWhenModelFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model"}]}`))
	}))
	defer server.Close()

	result := checkMLX(context.Background(), config.Config{
		MLXEnabled: true,
		MLXBaseURL: server.URL,
		MLXModel:   "model",
	})
	if result.Status != Pass {
		t.Fatalf("status = %s, want Pass", result.Status)
	}
}

func TestCheckMLXWarnsWhenModelMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"other"}]}`))
	}))
	defer server.Close()

	result := checkMLX(context.Background(), config.Config{
		MLXEnabled: true,
		MLXBaseURL: server.URL,
		MLXModel:   "model",
	})
	if result.Status != Warn {
		t.Fatalf("status = %s, want Warn", result.Status)
	}
}

func TestCheckServerReturnsPassForHealthz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	results := CheckServer(context.Background(), server.URL)
	if len(results) == 0 {
		t.Fatal("CheckServer() returned no results")
	}
	for _, r := range results {
		if r.Status != Pass {
			t.Fatalf("%s status = %s", r.Name, r.Status)
		}
	}
}

func TestCheckServerReturnsFailForUnreachable(t *testing.T) {
	results := CheckServer(context.Background(), "http://127.0.0.1:1")
	if len(results) == 0 {
		t.Fatal("CheckServer() returned no results")
	}
	for _, r := range results {
		if r.Status != Fail {
			t.Fatalf("%s status = %s, want Fail", r.Name, r.Status)
		}
	}
}

func TestCheckServerReturnsFailForNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	results := CheckServer(context.Background(), server.URL)
	for _, r := range results {
		if r.Status != Fail {
			t.Fatalf("%s status = %s, want Fail", r.Name, r.Status)
		}
	}
}

func TestRedactReplacesNewlines(t *testing.T) {
	got := redact("line1\nline2")
	if got != "line1 line2" {
		t.Fatalf("redact() = %q", got)
	}
}

func TestRedactTruncatesLongMessages(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	got := redact(string(long))
	if len(got) != 240 {
		t.Fatalf("redact() length = %d, want 240", len(got))
	}
}

func TestRedactLeavesShortMessages(t *testing.T) {
	got := redact("hello")
	if got != "hello" {
		t.Fatalf("redact() = %q", got)
	}
}

func TestOllamaUnavailableMessageReturnsKnownMessages(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("connection refused"), "Ollama not running. Start with: ollama serve"},
		{errors.New("connect: operation timed out"), "Ollama not running. Start with: ollama serve"},
		{errors.New("client.timeout exceeded"), "Ollama not running. Start with: ollama serve"},
		{errors.New("context deadline exceeded"), "Ollama not running. Start with: ollama serve"},
		{errors.New("some other error"), "some other error"},
	}
	for _, tt := range tests {
		if got := ollamaUnavailableMessage(tt.err); got != tt.want {
			t.Fatalf("ollamaUnavailableMessage(%q) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestCheckConfigFileWarnsWhenFileNotFound(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	t.Setenv("LINEA_ENV_FILE", envPath)

	result := checkConfigFile()

	if result.Status != Warn {
		t.Fatalf("checkConfigFile() status = %s, want Warn", result.Status)
	}
}

func TestConfigFilePassesForExistingDotEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("LINEA_ENV_FILE", envPath)

	result := checkConfigFile()

	if result.Status != Pass {
		t.Fatalf("checkConfigFile() status = %s, want Pass", result.Status)
	}
}

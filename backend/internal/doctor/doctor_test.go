package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
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

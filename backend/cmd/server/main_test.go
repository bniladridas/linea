package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"linea/backend/internal/api"
	"linea/backend/internal/config"
)

func TestProviderStatusesMarksOllamaReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:1.5b"}]}`))
	}))
	defer server.Close()

	status := localProviderStatus(t, config.Config{
		OllamaFallback: true,
		OllamaBaseURL:  server.URL,
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if !status.Enabled {
		t.Fatal("expected Ollama to be enabled")
	}
	if status.State != "ready" {
		t.Fatalf("expected ready state, got %q", status.State)
	}
}

func TestProviderStatusesMarksOllamaSleeping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	status := localProviderStatus(t, config.Config{
		OllamaFallback: true,
		OllamaBaseURL:  server.URL,
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if status.Enabled {
		t.Fatal("expected Ollama to be disabled")
	}
	if status.State != "sleeping" {
		t.Fatalf("expected sleeping state, got %q", status.State)
	}
	if status.Message == "" {
		t.Fatal("expected a status message")
	}
}

func TestProviderStatusesMarksOllamaOff(t *testing.T) {
	status := localProviderStatus(t, config.Config{
		OllamaFallback: false,
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if status.Enabled {
		t.Fatal("expected Ollama to be disabled")
	}
	if status.State != "off" {
		t.Fatalf("expected off state, got %q", status.State)
	}
}

func localProviderStatus(t *testing.T, cfg config.Config) api.ProviderStatus {
	t.Helper()
	for _, status := range providerStatuses(context.Background(), cfg) {
		if status.Role == "local" {
			return status
		}
	}
	t.Fatal("missing local provider")
	return api.ProviderStatus{}
}

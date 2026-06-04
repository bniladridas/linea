package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"linea/backend/internal/agent"
	"linea/backend/internal/api"
	"linea/backend/internal/config"
	"linea/backend/internal/llm"
	"linea/backend/internal/store"
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
	if status.Detail != "Run: ollama pull qwen2.5-coder:1.5b" {
		t.Fatalf("detail = %q", status.Detail)
	}
}

func TestProviderStatusesShowsOllamaStartInstruction(t *testing.T) {
	status := localProviderStatus(t, config.Config{
		OllamaFallback: true,
		OllamaBaseURL:  "http://127.0.0.1:1",
		OllamaModel:    "qwen2.5-coder:1.5b",
	})

	if status.Enabled {
		t.Fatal("expected Ollama to be disabled")
	}
	if status.State != "sleeping" {
		t.Fatalf("expected sleeping state, got %q", status.State)
	}
	if status.Message != "Ollama not running" {
		t.Fatalf("message = %q", status.Message)
	}
	if status.Detail != "Start with: ollama serve" {
		t.Fatalf("detail = %q", status.Detail)
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

func TestPrintAgentStatusWritesLocalContract(t *testing.T) {
	rulesPath := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(rulesPath, []byte("# Linea\n\n* Keep it local.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var out bytes.Buffer

	err := printAgentStatus(context.Background(), config.Config{AgentRulesPath: rulesPath}, &out)
	if err != nil {
		t.Fatalf("printAgentStatus() error = %v", err)
	}

	var status agent.Status
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if status.Mode != "local" || !status.Rules.Loaded || len(status.Tools) == 0 || len(status.Boundaries) == 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestProviderSettingsStorePersistsOrderAndToggles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	defaults := api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Name: "Gemini", Enabled: true, Configured: true},
		{ID: "ollama", Name: "Ollama", Enabled: true, Configured: true},
	}}
	settingsStore, err := newProviderSettingsStore(path, defaults)
	if err != nil {
		t.Fatalf("newProviderSettingsStore() error = %v", err)
	}

	updated, err := settingsStore.UpdateSettings(api.Settings{Providers: []api.ProviderSetting{
		{ID: "ollama", Enabled: true},
		{ID: "gemini", Enabled: false},
	}})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if updated.Providers[0].ID != "ollama" || updated.Providers[1].Enabled {
		t.Fatalf("updated settings = %#v", updated)
	}

	reloaded, err := newProviderSettingsStore(path, defaults)
	if err != nil {
		t.Fatalf("reload settings error = %v", err)
	}
	got := reloaded.GetSettings()
	if got.Providers[0].ID != "ollama" || got.Providers[1].Enabled {
		t.Fatalf("reloaded settings = %#v", got)
	}
}

func TestProviderSettingsStoreKeepsCurrentWhenSaveFails(t *testing.T) {
	defaults := api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Name: "Gemini", Enabled: true, Configured: true},
		{ID: "ollama", Name: "Ollama", Enabled: true, Configured: true},
	}}
	settingsStore := &providerSettingsStore{
		path:     t.TempDir(),
		defaults: defaults,
		current:  defaults,
	}

	_, err := settingsStore.UpdateSettings(api.Settings{Providers: []api.ProviderSetting{
		{ID: "ollama", Enabled: true},
		{ID: "gemini", Enabled: false},
	}})
	if err == nil {
		t.Fatal("expected UpdateSettings() to fail")
	}
	got := settingsStore.GetSettings()
	if got.Providers[0].ID != "gemini" || !got.Providers[0].Enabled || got.Providers[1].ID != "ollama" {
		t.Fatalf("settings changed after failed save = %#v", got)
	}
}

func TestRoutingAssistantUsesGeminiForImageAttachments(t *testing.T) {
	settingsStore := &providerSettingsStore{
		current: api.Settings{Providers: []api.ProviderSetting{
			{ID: "cerebras", Enabled: true, Configured: true},
			{ID: "gemini", Enabled: true, Configured: true},
		}},
	}
	cerebras := &recordingStreamer{err: errors.New("cerebras should be skipped")}
	gemini := &recordingStreamer{chunks: []string{"ok"}}
	assistant := &routingAssistant{
		settings: settingsStore,
		clients: map[string]llm.Streamer{
			"cerebras": cerebras,
			"gemini":   gemini,
		},
	}

	var got string
	err := assistant.GenerateStream(
		context.Background(),
		[]store.Message{{Role: "user", Content: "describe this"}},
		[]llm.Attachment{{Name: "image.png", MIMEType: "image/png", Data: []byte("png")}},
		nil,
		func(chunk string) error {
			got += chunk
			return nil
		},
	)
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if cerebras.calls != 0 {
		t.Fatalf("text-only provider was called %d time(s)", cerebras.calls)
	}
	if gemini.calls != 1 || got != "ok" {
		t.Fatalf("gemini calls = %d, response = %q", gemini.calls, got)
	}
}

func TestRoutingAssistantPreservesProviderOrderForText(t *testing.T) {
	settingsStore := &providerSettingsStore{
		current: api.Settings{Providers: []api.ProviderSetting{
			{ID: "cerebras", Enabled: true, Configured: true},
			{ID: "gemini", Enabled: true, Configured: true},
		}},
	}
	var order []string
	cerebras := &recordingStreamer{name: "cerebras", order: &order, err: errors.New("try fallback")}
	gemini := &recordingStreamer{name: "gemini", order: &order, chunks: []string{"ok"}}
	assistant := &routingAssistant{
		settings: settingsStore,
		clients: map[string]llm.Streamer{
			"cerebras": cerebras,
			"gemini":   gemini,
		},
	}

	err := assistant.GenerateStream(context.Background(), []store.Message{{Role: "user", Content: "hello"}}, nil, nil, func(string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if len(order) != 2 || order[0] != "cerebras" || order[1] != "gemini" {
		t.Fatalf("provider order = %#v", order)
	}
}

func TestProviderStatusesFollowSettingsOrder(t *testing.T) {
	settings := api.Settings{Providers: []api.ProviderSetting{
		{ID: "ollama", Enabled: true, Configured: true},
		{ID: "gemini", Enabled: false, Configured: true},
		{ID: "cerebras", Enabled: true, Configured: true},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:1.5b"}]}`))
	}))
	defer server.Close()

	statuses := providerStatuses(context.Background(), config.Config{
		GeminiAPIKey:    "key",
		GeminiModel:     "gemini",
		CerebrasEnabled: true,
		CerebrasAPIKey:  "key",
		CerebrasModel:   "cerebras",
		OllamaFallback:  true,
		OllamaBaseURL:   server.URL,
		OllamaModel:     "qwen2.5-coder:1.5b",
	}, settings)

	if len(statuses) != 3 {
		t.Fatalf("status count = %d", len(statuses))
	}
	if statuses[0].Name != "Ollama" || statuses[1].Name != "Gemini" || statuses[2].Name != "Cerebras" {
		t.Fatalf("status order = %#v", statuses)
	}
	if statuses[1].Message != "off" {
		t.Fatalf("disabled configured message = %q", statuses[1].Message)
	}
}

type recordingStreamer struct {
	name   string
	order  *[]string
	chunks []string
	err    error
	calls  int
}

func (s *recordingStreamer) GenerateStream(_ context.Context, _ []store.Message, _ []llm.Attachment, _ []llm.SearchResult, onChunk func(string) error) error {
	s.calls++
	if s.order != nil {
		*s.order = append(*s.order, s.name)
	}
	for _, chunk := range s.chunks {
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	return s.err
}

func localProviderStatus(t *testing.T, cfg config.Config) api.ProviderStatus {
	t.Helper()
	for _, status := range providerStatuses(context.Background(), cfg, defaultProviderSettings(cfg)) {
		if status.Role == "local" {
			return status
		}
	}
	t.Fatal("missing local provider")
	return api.ProviderStatus{}
}

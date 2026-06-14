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
	"strings"
	"testing"

	"linea/backend/internal/agent"
	"linea/backend/internal/api"
	"linea/backend/internal/config"
	"linea/backend/internal/doctor"
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

func TestIsPublicAPIAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{name: "loopback ipv4", addr: "127.0.0.1:8080", want: false},
		{name: "localhost", addr: "localhost:8080", want: false},
		{name: "loopback ipv6", addr: "[::1]:8080", want: false},
		{name: "port only", addr: ":8080", want: true},
		{name: "all ipv4", addr: "0.0.0.0:8080", want: true},
		{name: "all ipv6", addr: "[::]:8080", want: true},
		{name: "lan ipv4", addr: "192.168.1.20:8080", want: true},
		{name: "hostname", addr: "linea.local:8080", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPublicAPIAddr(tt.addr); got != tt.want {
				t.Fatalf("isPublicAPIAddr(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIsPublicAddrRejectsLocalhostCaseInsensitive(t *testing.T) {
	if isPublicAPIAddr("LOCALHOST:8080") {
		t.Fatal("isPublicAPIAddr(LOCALHOST:8080) = true, want false")
	}
}

func TestIsPublicAddrTreatsHostWithoutPortAsPublic(t *testing.T) {
	if !isPublicAPIAddr("no-port") {
		t.Fatal("isPublicAPIAddr(no-port) = false, want true (hostname without port is treated as public)")
	}
}

func TestState(t *testing.T) {
	tests := []struct {
		enabled bool
		want    string
	}{
		{true, "ready"},
		{false, "off"},
	}
	for _, tt := range tests {
		if got := state(tt.enabled); got != tt.want {
			t.Fatalf("state(%v) = %q, want %q", tt.enabled, got, tt.want)
		}
	}
}

func TestConfiguredMessage(t *testing.T) {
	tests := []struct {
		name       string
		configured bool
		enabled    bool
		want       string
	}{
		{name: "not configured", configured: false, enabled: false, want: "not configured"},
		{name: "off", configured: true, enabled: false, want: "off"},
		{name: "configured", configured: true, enabled: true, want: "configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configuredMessage(tt.enabled, tt.configured); got != tt.want {
				t.Fatalf("configuredMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocalFallbackState(t *testing.T) {
	tests := []struct {
		ready    bool
		enabled  bool
		want     string
	}{
		{ready: true, enabled: true, want: "ready"},
		{ready: false, enabled: true, want: "sleeping"},
		{ready: false, enabled: false, want: "off"},
	}
	for _, tt := range tests {
		if got := localFallbackState(tt.enabled, tt.ready); got != tt.want {
			t.Fatalf("localFallbackState(%v, %v) = %q, want %q", tt.enabled, tt.ready, got, tt.want)
		}
	}
}

func TestIsOllamaNotRunning(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"Ollama not running", true},
		{"connection refused", true},
		{"connect: operation timed out", true},
		{"client.timeout exceeded", true},
		{"context deadline exceeded", true},
		{"is not installed", false},
		{"model not found", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isOllamaNotRunning(tt.msg); got != tt.want {
			t.Fatalf("isOllamaNotRunning(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestLocalFallbackMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"Ollama not running", "Ollama not running"},
		{"connection refused", "Ollama not running"},
		{"is not installed", "Model not installed"},
		{"some other error", "some other error"},
		{"", "local fallback unavailable"},
	}
	for _, tt := range tests {
		if got := localFallbackMessage(tt.msg); got != tt.want {
			t.Fatalf("localFallbackMessage(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestLocalFallbackDetail(t *testing.T) {
	cfg := config.Config{OllamaModel: "qwen2.5-coder:1.5b"}
	tests := []struct {
		name        string
		routeOn     bool
		result      doctor.Result
		want        string
	}{
		{name: "off", routeOn: false, want: "Local fallback is off."},
		{name: "ready", routeOn: true, result: doctor.Result{Status: doctor.Pass}, want: "qwen2.5-coder:1.5b is available."},
		{name: "not running", routeOn: true, result: doctor.Result{Status: doctor.Warn, Message: "Ollama not running"}, want: "Start with: ollama serve"},
		{name: "not installed", routeOn: true, result: doctor.Result{Status: doctor.Warn, Message: "is not installed"}, want: "Run: ollama pull qwen2.5-coder:1.5b"},
		{name: "other", routeOn: true, result: doctor.Result{Status: doctor.Warn, Message: "unknown error"}, want: "unknown error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localFallbackDetail(cfg, tt.routeOn, doctor.Result{Status: tt.result.Status, Message: tt.result.Message}); got != tt.want {
				t.Fatalf("localFallbackDetail() = %q, want %q", got, tt.want)
			}
		})
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

func TestProviderSettingsStoreRequiresOneEnabled(t *testing.T) {
	settingsStore := &providerSettingsStore{
		current: api.Settings{Providers: []api.ProviderSetting{
			{ID: "gemini", Enabled: true, Configured: true},
		}},
	}
	_, err := settingsStore.UpdateSettings(api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Enabled: false},
	}})
	if err == nil {
		t.Fatal("expected error when disabling last provider")
	}
}

func TestProviderSettingsStoreWorksWithoutPath(t *testing.T) {
	defaults := api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Enabled: true, Configured: true},
	}}
	store, err := newProviderSettingsStore("", defaults)
	if err != nil {
		t.Fatalf("newProviderSettingsStore() error = %v", err)
	}
	got := store.GetSettings()
	if len(got.Providers) != 1 {
		t.Fatalf("GetSettings() = %#v", got)
	}
}

func TestProviderSettingsStoreNormalizes(t *testing.T) {
	defaults := api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Name: "Gemini", Configured: true},
		{ID: "ollama", Name: "Ollama", Configured: false},
	}}
	store, err := newProviderSettingsStore("", defaults)
	if err != nil {
		t.Fatalf("newProviderSettingsStore() error = %v", err)
	}

	updated, err := store.UpdateSettings(api.Settings{Providers: []api.ProviderSetting{
		{ID: "ollama", Enabled: true},
		{ID: "gemini", Enabled: true},
	}})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if !updated.Providers[1].Enabled {
		t.Fatal("gemini should be enabled")
	}
	if updated.Providers[0].Enabled {
		t.Fatal("ollama should be disabled (not configured)")
	}
}

func TestProviderSettingsStoreIgnoresUnknownIDs(t *testing.T) {
	defaults := api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Name: "Gemini", Enabled: true, Configured: true},
	}}
	store, _ := newProviderSettingsStore("", defaults)

	updated, err := store.UpdateSettings(api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Enabled: false},
		{ID: "unknown", Enabled: true},
	}})
	if err == nil {
		t.Fatal("expected error when disabling last provider")
	}
	_ = updated
}

func TestHasEnabledConfiguredProvider(t *testing.T) {
	tests := []struct {
		name     string
		settings api.Settings
		want     bool
	}{
		{name: "one enabled and configured", settings: api.Settings{Providers: []api.ProviderSetting{
			{ID: "a", Enabled: true, Configured: true},
		}}, want: true},
		{name: "enabled but not configured", settings: api.Settings{Providers: []api.ProviderSetting{
			{ID: "a", Enabled: true, Configured: false},
		}}, want: false},
		{name: "none enabled", settings: api.Settings{Providers: []api.ProviderSetting{
			{ID: "a", Enabled: false, Configured: true},
		}}, want: false},
		{name: "empty", settings: api.Settings{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasEnabledConfiguredProvider(tt.settings); got != tt.want {
				t.Fatalf("hasEnabledConfiguredProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultProviderSettings(t *testing.T) {
	settings := defaultProviderSettings(config.Config{
		GeminiAPIKey:       "key",
		GeminiModel:        "gemini",
		SambaNovaEnabled:   true,
		SambaNovaAPIKey:    "samba",
		CerebrasEnabled:    false,
		OllamaFallback:     true,
		OllamaModel:        "qwen",
	})
	if len(settings.Providers) != 4 {
		t.Fatalf("got %d providers, want 4", len(settings.Providers))
	}
	for _, p := range settings.Providers {
		switch p.ID {
		case "gemini":
			if !p.Enabled || !p.Configured {
				t.Fatalf("gemini: enabled=%v configured=%v", p.Enabled, p.Configured)
			}
		case "sambanova":
			if !p.Enabled || !p.Configured {
				t.Fatalf("sambanova: enabled=%v configured=%v", p.Enabled, p.Configured)
			}
		case "cerebras":
			if p.Enabled || p.Configured {
				t.Fatalf("cerebras: enabled=%v configured=%v", p.Enabled, p.Configured)
			}
		case "ollama":
			if !p.Enabled || !p.Configured {
				t.Fatalf("ollama: enabled=%v configured=%v", p.Enabled, p.Configured)
			}
		}
	}
}

func TestProviderSupportsImages(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"gemini", true},
		{"sambanova", false},
		{"cerebras", false},
		{"ollama", false},
	}
	for _, tt := range tests {
		if got := providerSupportsImages(tt.id); got != tt.want {
			t.Fatalf("providerSupportsImages(%q) = %v, want %v", tt.id, got, tt.want)
		}
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

func TestStripPlannerJSONKeepsInnerMarkdownFence(t *testing.T) {
	input := "```json\n{\"path\":\"README.md\",\"content\":\"before\\n```go\\nfmt.Println(1)\\n```\\nafter\\n\",\"summary\":\"docs\"}\n```"
	got := stripPlannerJSON(input)
	if !strings.Contains(got, "\\n```go\\n") {
		t.Fatalf("stripped JSON lost inner fence: %q", got)
	}
	var plan agent.EditPlan
	if err := json.Unmarshal([]byte(got), &plan); err != nil {
		t.Fatalf("Unmarshal() error = %v; json = %q", err, got)
	}
	if plan.Content != "before\n```go\nfmt.Println(1)\n```\nafter\n" {
		t.Fatalf("content = %q", plan.Content)
	}
}

func TestStripPlannerJSONUsesFirstCompleteObject(t *testing.T) {
	input := "planner output\n{\"path\":\"portfolio.html\",\"content\":\"body { color: red; }\",\"summary\":\"page\"}{\"extra\":true}"
	got := stripPlannerJSON(input)
	var plan agent.EditPlan
	if err := json.Unmarshal([]byte(got), &plan); err != nil {
		t.Fatalf("Unmarshal() error = %v; json = %q", err, got)
	}
	if plan.Path != "portfolio.html" || plan.Content != "body { color: red; }" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestStripPlannerJSONHandlesNoFence(t *testing.T) {
	input := `{"path":"test.txt","content":"hello","summary":"test"}`
	got := stripPlannerJSON(input)
	var plan agent.EditPlan
	if err := json.Unmarshal([]byte(got), &plan); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if plan.Path != "test.txt" || plan.Content != "hello" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestStripPlannerJSONHandlesEmpty(t *testing.T) {
	if got := stripPlannerJSON(""); got != "" {
		t.Fatalf("stripPlannerJSON('') = %q", got)
	}
}

func TestFirstPlannerJSONObject(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: `{"a":1}`, want: `{"a":1}`},
		{input: `prefix {"a":1} suffix`, want: `{"a":1}`},
		{input: `{"nested":{"inner":"value"}}`, want: `{"nested":{"inner":"value"}}`},
		{input: `no object`, want: `no object`},
		{input: `{"escaped\\"quote":true}`, want: `{"escaped\\"quote":true}`},
	}
	for _, tt := range tests {
		if got := firstPlannerJSONObject(tt.input); got != tt.want {
			t.Fatalf("firstPlannerJSONObject(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildEditPlannerPromptConstrainsTempAppEdits(t *testing.T) {
	prompt := buildEditPlannerPrompt(agent.EditPlanRequest{
		Goal: "make the preview background blue",
		Files: []agent.FileResult{{
			Path:    "src/App.jsx",
			Content: "export default function App() {}",
		}},
	})

	for _, want := range []string{
		"temporary React app preview",
		"Return only path src/App.jsx",
		"JSX is allowed",
		"Do not import network resources",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildEditPlannerPromptIncludesGoal(t *testing.T) {
	prompt := buildEditPlannerPrompt(agent.EditPlanRequest{
		Goal: "add a button",
	})
	if !strings.Contains(prompt, "add a button") {
		t.Fatal("prompt missing goal")
	}
}

func TestBuildEditPlannerPromptIncludesFiles(t *testing.T) {
	prompt := buildEditPlannerPrompt(agent.EditPlanRequest{
		Goal: "update test",
		Files: []agent.FileResult{{
			Path:    "file.go",
			Content: "package main",
		}},
	})
	if !strings.Contains(prompt, "file.go") || !strings.Contains(prompt, "package main") {
		t.Fatal("prompt missing file info")
	}
}

func TestBuildEditPlannerPromptTruncatesLargeContent(t *testing.T) {
	large := make([]byte, 30000)
	for i := range large {
		large[i] = 'x'
	}
	prompt := buildEditPlannerPrompt(agent.EditPlanRequest{
		Goal: "fix it",
		Files: []agent.FileResult{{
			Path:    "big.go",
			Content: string(large),
		}},
	})
	if !strings.Contains(prompt, "... truncated ...") {
		t.Fatal("prompt should truncate large content")
	}
}

func TestIsTempAppPlannerRequest(t *testing.T) {
	tests := []struct {
		name  string
		input agent.EditPlanRequest
		want  bool
	}{
		{name: "single app file", input: agent.EditPlanRequest{
			Files: []agent.FileResult{{Path: "src/App.jsx"}},
		}, want: true},
		{name: "multiple files", input: agent.EditPlanRequest{
			Files: []agent.FileResult{{Path: "src/App.jsx"}, {Path: "other.go"}},
		}, want: false},
		{name: "different path", input: agent.EditPlanRequest{
			Files: []agent.FileResult{{Path: "index.js"}},
		}, want: false},
		{name: "no files", input: agent.EditPlanRequest{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTempAppPlannerRequest(tt.input); got != tt.want {
				t.Fatalf("isTempAppPlannerRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrimPlannerText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit int
		want  string
	}{
		{name: "short", input: "hello", limit: 10, want: "hello"},
		{name: "exact", input: "hello", limit: 5, want: "hello"},
		{name: "truncated", input: "hello world", limit: 5, want: "hello\n... truncated ..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimPlannerText(tt.input, tt.limit); got != tt.want {
				t.Fatalf("trimPlannerText() = %q, want %q", got, tt.want)
			}
		})
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

func TestRoutingAssistantReturnsErrorWhenNoProvider(t *testing.T) {
	settingsStore := &providerSettingsStore{
		current: api.Settings{Providers: []api.ProviderSetting{
			{ID: "gemini", Enabled: false, Configured: true},
		}},
	}
	assistant := &routingAssistant{
		settings: settingsStore,
		clients:  map[string]llm.Streamer{},
	}
	err := assistant.GenerateStream(context.Background(), nil, nil, nil, nil)
	if err != llm.ErrMissingAPIKey {
		t.Fatalf("GenerateStream() error = %v, want ErrMissingAPIKey", err)
	}
}

func TestRoutingAssistantSkipsDisabledProviders(t *testing.T) {
	settingsStore := &providerSettingsStore{
		current: api.Settings{Providers: []api.ProviderSetting{
			{ID: "gemini", Enabled: true, Configured: true},
			{ID: "ollama", Enabled: false, Configured: true},
		}},
	}
	called := false
	gemini := &recordingStreamer{chunks: []string{"ok"}}
	assistant := &routingAssistant{
		settings: settingsStore,
		clients: map[string]llm.Streamer{
			"gemini": gemini,
			"ollama": &recordingStreamer{err: errors.New("should not be called")},
		},
	}
	err := assistant.GenerateStream(context.Background(), []store.Message{{Role: "user", Content: "hi"}}, nil, nil, func(string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if !called {
		t.Fatal("onChunk was not called")
	}
}

func TestAppStatusStorageTypes(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{name: "sync", cfg: config.Config{SyncURL: "https://sync.example.com"}, want: "Remote"},
		{name: "database", cfg: config.Config{DatabaseURL: "postgres://localhost"}, want: "PostgreSQL"},
		{name: "memory", cfg: config.Config{}, want: "Memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := appStatus(context.Background(), tt.cfg, api.Settings{})
			if s.Storage != tt.want {
				t.Fatalf("Storage = %q, want %q", s.Storage, tt.want)
			}
		})
	}
}

func TestProviderStatusesHandlesEmptySettings(t *testing.T) {
	statuses := providerStatuses(context.Background(), config.Config{}, api.Settings{})
	if len(statuses) != 0 {
		t.Fatalf("status count = %d, want 0", len(statuses))
	}
}

func TestProviderStatusesHandlesOllamaWithRouteOff(t *testing.T) {
	settings := api.Settings{Providers: []api.ProviderSetting{
		{ID: "ollama", Enabled: false},
	}}
	statuses := providerStatuses(context.Background(), config.Config{OllamaFallback: true}, settings)
	var ollama *api.ProviderStatus
	for _, s := range statuses {
		if s.Name == "Ollama" {
			ollama = &s
			break
		}
	}
	if ollama != nil && ollama.Enabled {
		t.Fatal("Ollama should be disabled")
	}
}

func TestConfiguredProviderStatus(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		keySet  bool
		wantEna bool
		wantMsg string
	}{
		{name: "enabled and key set", enabled: true, keySet: true, wantEna: true, wantMsg: "configured"},
		{name: "enabled but no key", enabled: true, keySet: false, wantEna: false, wantMsg: "not configured"},
		{name: "disabled with key", enabled: false, keySet: true, wantEna: false, wantMsg: "off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := configuredProviderStatus("Test", "model", "primary", tt.enabled, tt.keySet)
			if status.Enabled != tt.wantEna {
				t.Fatalf("Enabled = %v, want %v", status.Enabled, tt.wantEna)
			}
			if status.Message != tt.wantMsg {
				t.Fatalf("Message = %q, want %q", status.Message, tt.wantMsg)
			}
		})
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

func TestAppStatusSearch(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
	}{
		{name: "brave", cfg: config.Config{BraveSearchAPIKey: "key"}},
		{name: "searxng", cfg: config.Config{SearXNGURL: "http://127.0.0.1:8888"}},
		{name: "none", cfg: config.Config{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := appStatus(context.Background(), tt.cfg, api.Settings{})
			if s.Search == "" {
				t.Fatal("Search is empty")
			}
		})
	}
}

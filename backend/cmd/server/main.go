package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"linea/backend/internal/agent"
	"linea/backend/internal/api"
	"linea/backend/internal/config"
	"linea/backend/internal/doctor"
	"linea/backend/internal/llm"
	"linea/backend/internal/migrate"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
	"linea/backend/internal/tui"
	"linea/backend/internal/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	runMigrations := flag.Bool("migrate", false, "apply database migrations and exit")
	runCheck := flag.Bool("check", false, "run non-interactive health checks and exit")
	runAgentStatus := flag.Bool("agent-status", false, "print local agent status and exit")
	runTUI := flag.Bool("tui", false, "run the terminal chat interface")
	runTUIBeta := flag.Bool("tui-beta", false, "run the hand-rolled terminal chat interface")
	checkServerURL := flag.String("check-server", "", "check a running Linea server URL and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("linea %s\n", version)
		return
	}

	if err := config.LoadEnvFile(); err != nil {
		slog.Error("load env file", "error", err)
		os.Exit(1)
	}
	cfg := config.Load()
	ctx := context.Background()
	if *runMigrations {
		migrateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		results, err := migrate.Run(migrateCtx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("migrate", "error", err)
			os.Exit(1)
		}
		for _, result := range results {
			status := "skipped"
			if result.Applied {
				status = "applied"
			}
			fmt.Printf("%s %s\n", status, result.Name)
		}
		return
	}
	if *runCheck {
		checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		results := doctor.Run(checkCtx, cfg)
		doctor.Print(results)
		if doctor.HasFailure(results) {
			os.Exit(1)
		}
		return
	}
	if *checkServerURL != "" {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		results := doctor.CheckServer(checkCtx, *checkServerURL)
		doctor.Print(results)
		if doctor.HasFailure(results) {
			os.Exit(1)
		}
		return
	}
	if *runAgentStatus {
		if err := printAgentStatus(ctx, cfg, os.Stdout); err != nil {
			slog.Error("agent status", "error", err)
			os.Exit(1)
		}
		return
	}

	appStore := store.Store(store.NewMemoryStore())
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("connect postgres", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		if err := pool.Ping(ctx); err != nil {
			slog.Error("ping postgres", "error", err)
			os.Exit(1)
		}
		appStore = store.NewPostgresStore(pool)
	} else {
		slog.Warn("DATABASE_URL not set; using in-memory storage")
	}

	staticFiles := web.Files()
	if cfg.StaticDir != "" {
		staticFiles = http.Dir(cfg.StaticDir)
	}
	settingsStore, err := newProviderSettingsStore(config.DefaultSettingsFilePath(), defaultProviderSettings(cfg))
	if err != nil {
		slog.Error("load settings", "error", err)
		os.Exit(1)
	}
	llmClient := newRoutingAssistant(cfg, settingsStore)
	agentRuntime := newAgentRuntime(cfg, llmEditPlanner{assistant: llmClient})
	if *runTUIBeta {
		if err := tui.New(appStore, llmClient, os.Stdin, os.Stdout).WithSearcher(search.NewClient()).WithAgentRuntime(agentRuntime).RunBeta(ctx); err != nil {
			slog.Error("hand-rolled tui", "error", err)
			os.Exit(1)
		}
		return
	}
	if *runTUI {
		if err := tui.New(appStore, llmClient, os.Stdin, os.Stdout).WithSearcher(search.NewClient()).WithAgentRuntime(agentRuntime).Run(ctx); err != nil {
			slog.Error("tui", "error", err)
			os.Exit(1)
		}
		return
	}
	server := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           api.NewServerWithAgentRuntime(appStore, llmClient, search.NewClient(), staticFiles, cfg.WebOrigin, func(ctx context.Context) api.Status { return appStatus(ctx, cfg, settingsStore.GetSettings()) }, settingsStore, agentRuntime).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("linea api listening", "addr", cfg.APIAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "error", err)
			os.Exit(1)
		}
	}()

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-shutdownCtx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "error", err)
		os.Exit(1)
	}
}

func printAgentStatus(ctx context.Context, cfg config.Config, out io.Writer) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(newAgentRuntime(cfg).Status(ctx))
}

func newAgentRuntime(cfg config.Config, planners ...agent.EditPlanner) *agent.Runtime {
	options := []func(*agent.Runtime){
		agent.WithWorkspaceRoot(cfg.AgentWorkspaceDir),
		agent.WithSkillsDir(cfg.AgentSkillsDir),
		agent.WithMCPConfigPath(cfg.AgentMCPConfig),
		agent.WithCommandAllowlist(cfg.AgentCommandAllowlist),
	}
	if len(planners) > 0 && planners[0] != nil {
		options = append(options, agent.WithEditPlanner(planners[0]))
	}
	return agent.NewRuntime(
		cfg.AgentRulesPath,
		options...,
	)
}

type llmEditPlanner struct {
	assistant interface {
		GenerateStream(context.Context, []store.Message, []llm.Attachment, []llm.SearchResult, func(string) error) error
	}
}

func (p llmEditPlanner) PlanEdit(ctx context.Context, request agent.EditPlanRequest) (agent.EditPlan, error) {
	if p.assistant == nil {
		return agent.EditPlan{}, errors.New("edit planner is unavailable")
	}
	prompt := buildEditPlannerPrompt(request)
	var out strings.Builder
	err := p.assistant.GenerateStream(ctx, []store.Message{{
		ID:      store.NewID(),
		Role:    "user",
		Content: prompt,
	}}, nil, nil, func(chunk string) error {
		_, writeErr := out.WriteString(chunk)
		return writeErr
	})
	if err != nil {
		return agent.EditPlan{}, err
	}
	var plan agent.EditPlan
	if err := json.Unmarshal([]byte(stripPlannerJSON(out.String())), &plan); err != nil {
		return agent.EditPlan{}, fmt.Errorf("parse edit plan: %w", err)
	}
	if strings.TrimSpace(plan.Path) == "" {
		return agent.EditPlan{}, errors.New("edit plan path is required")
	}
	return plan, nil
}

func buildEditPlannerPrompt(request agent.EditPlanRequest) string {
	var builder strings.Builder
	builder.WriteString("You are Linea's local edit planner. Return only JSON with path, content, and summary. ")
	builder.WriteString("Content must be the complete replacement text for one existing file. Do not include markdown fences.\n\n")
	builder.WriteString("Goal:\n")
	builder.WriteString(request.Goal)
	builder.WriteString("\n\n")
	if request.Command != "" || request.CommandOutput != "" {
		builder.WriteString("Command:\n")
		builder.WriteString(request.Command)
		builder.WriteString("\n\nCommand output:\n")
		builder.WriteString(trimPlannerText(request.CommandOutput, 6000))
		builder.WriteString("\n\n")
	}
	if len(request.Diagnostics) > 0 {
		builder.WriteString("Diagnostics:\n")
		for _, diagnostic := range request.Diagnostics {
			builder.WriteString(fmt.Sprintf("- %s:%d:%d %s\n", diagnostic.Path, diagnostic.Line, diagnostic.Column, diagnostic.Message))
		}
		builder.WriteString("\n")
	}
	for _, file := range request.Files {
		builder.WriteString("File: ")
		builder.WriteString(file.Path)
		if file.Truncated {
			builder.WriteString(" (truncated)")
		}
		builder.WriteString("\n```")
		builder.WriteString("\n")
		builder.WriteString(trimPlannerText(file.Content, 20000))
		builder.WriteString("\n```\n\n")
	}
	builder.WriteString(`Return shape: {"path":"relative/path","content":"complete file content","summary":"short summary"}`)
	return builder.String()
}

func stripPlannerJSON(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	_, rest, ok := strings.Cut(value, "\n")
	if !ok {
		return value
	}
	index := strings.LastIndex(rest, "```")
	if index < 0 {
		return value
	}
	return strings.TrimSpace(rest[:index])
}

func trimPlannerText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n... truncated ..."
}

func providerStatuses(ctx context.Context, cfg config.Config, settings api.Settings) []api.ProviderStatus {
	settingsByID := make(map[string]api.ProviderSetting, len(settings.Providers))
	for _, provider := range settings.Providers {
		settingsByID[provider.ID] = provider
	}
	ollama := doctor.Result{Status: doctor.Warn, Message: "fallback disabled"}
	ollamaSetting := settingsByID["ollama"]
	if cfg.OllamaFallback && ollamaSetting.Enabled {
		statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		ollama = doctor.CheckOllamaLocalModel(statusCtx, cfg)
	}
	ollamaRouteEnabled := cfg.OllamaFallback && ollamaSetting.Enabled
	ollamaEnabled := ollamaRouteEnabled && ollama.Status == doctor.Pass
	ollamaDetail := localFallbackDetail(cfg, ollamaRouteEnabled, ollama)
	if !ollamaRouteEnabled {
		ollama.Message = "off"
	} else if cfg.OllamaFallback {
		ollama.Message = localFallbackMessage(ollama.Message)
	}
	ollamaState := localFallbackState(ollamaRouteEnabled, ollamaEnabled)

	statusByID := map[string]api.ProviderStatus{
		"gemini":    configuredProviderStatus("Gemini", cfg.GeminiModel, "primary", settingsByID["gemini"].Enabled, cfg.GeminiAPIKey != ""),
		"sambanova": configuredProviderStatus("SambaNova", cfg.SambaNovaModel, "fallback", settingsByID["sambanova"].Enabled, cfg.SambaNovaEnabled && cfg.SambaNovaAPIKey != ""),
		"cerebras":  configuredProviderStatus("Cerebras", cfg.CerebrasModel, "fallback", settingsByID["cerebras"].Enabled, cfg.CerebrasEnabled && cfg.CerebrasAPIKey != ""),
		"ollama": api.ProviderStatus{
			Name:    "Ollama",
			Model:   cfg.OllamaModel,
			Enabled: ollamaEnabled,
			Role:    "local",
			State:   ollamaState,
			Message: ollama.Message,
			Detail:  ollamaDetail,
		},
	}
	statuses := make([]api.ProviderStatus, 0, len(settings.Providers))
	for _, provider := range settings.Providers {
		status, ok := statusByID[provider.ID]
		if ok {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func appStatus(ctx context.Context, cfg config.Config, settings api.Settings) api.Status {
	status := api.Status{
		Storage:   "PostgreSQL",
		Search:    "DuckDuckGo",
		Providers: providerStatuses(ctx, cfg, settings),
	}
	if cfg.DatabaseURL == "" {
		status.Storage = "Memory"
	}
	return status
}

func configuredProviderStatus(name, model, role string, routeEnabled, configured bool) api.ProviderStatus {
	enabled := routeEnabled && configured
	return api.ProviderStatus{
		Name:    name,
		Model:   model,
		Enabled: enabled,
		Role:    role,
		State:   state(enabled),
		Message: configuredMessage(routeEnabled, configured),
	}
}

func localFallbackState(fallbackEnabled, ready bool) string {
	if ready {
		return "ready"
	}
	if fallbackEnabled {
		return "sleeping"
	}
	return "off"
}

func localFallbackMessage(message string) string {
	if isOllamaNotRunning(message) {
		return "Ollama not running"
	}
	if strings.Contains(message, "is not installed") {
		return "Model not installed"
	}
	if message != "" {
		return message
	}
	return "local fallback unavailable"
}

func localFallbackDetail(cfg config.Config, routeEnabled bool, result doctor.Result) string {
	if !routeEnabled {
		return "Local fallback is off."
	}
	if result.Status == doctor.Pass {
		return cfg.OllamaModel + " is available."
	}
	if isOllamaNotRunning(result.Message) {
		return "Start with: ollama serve"
	}
	if strings.Contains(result.Message, "is not installed") {
		return "Run: ollama pull " + cfg.OllamaModel
	}
	return result.Message
}

func isOllamaNotRunning(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "ollama not running") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connect: operation timed out") ||
		strings.Contains(message, "client.timeout") ||
		strings.Contains(message, "context deadline exceeded")
}

func state(enabled bool) string {
	if enabled {
		return "ready"
	}
	return "off"
}

func configuredMessage(routeEnabled, configured bool) string {
	if !configured {
		return "not configured"
	}
	if !routeEnabled {
		return "off"
	}
	return "configured"
}

type providerSettingsStore struct {
	mu       sync.Mutex
	path     string
	defaults api.Settings
	current  api.Settings
}

func newProviderSettingsStore(path string, defaults api.Settings) (*providerSettingsStore, error) {
	store := &providerSettingsStore{
		path:     path,
		defaults: defaults,
		current:  defaults,
	}
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var saved api.Settings
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, err
	}
	store.current = store.normalize(saved)
	return store, nil
}

func (s *providerSettingsStore) GetSettings() api.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSettings(s.current)
}

func (s *providerSettingsStore) UpdateSettings(next api.Settings) (api.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := s.normalize(next)
	if !hasEnabledConfiguredProvider(normalized) {
		return api.Settings{}, errors.New("At least one configured provider must stay enabled.")
	}
	if err := s.saveLocked(normalized); err != nil {
		return api.Settings{}, err
	}
	s.current = normalized
	return cloneSettings(s.current), nil
}

func (s *providerSettingsStore) normalize(next api.Settings) api.Settings {
	defaultByID := make(map[string]api.ProviderSetting, len(s.defaults.Providers))
	for _, provider := range s.defaults.Providers {
		defaultByID[provider.ID] = provider
	}

	seen := make(map[string]bool, len(next.Providers))
	normalized := api.Settings{Providers: make([]api.ProviderSetting, 0, len(s.defaults.Providers))}
	for _, incoming := range next.Providers {
		base, ok := defaultByID[incoming.ID]
		if !ok || seen[incoming.ID] {
			continue
		}
		base.Enabled = incoming.Enabled && base.Configured
		normalized.Providers = append(normalized.Providers, base)
		seen[incoming.ID] = true
	}
	for _, provider := range s.defaults.Providers {
		if !seen[provider.ID] {
			normalized.Providers = append(normalized.Providers, provider)
		}
	}
	return normalized
}

func (s *providerSettingsStore) saveLocked(settings api.Settings) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o600)
}

func defaultProviderSettings(cfg config.Config) api.Settings {
	return api.Settings{Providers: []api.ProviderSetting{
		{ID: "gemini", Name: "Gemini", Model: cfg.GeminiModel, Role: "primary", Enabled: cfg.GeminiAPIKey != "", Configured: cfg.GeminiAPIKey != ""},
		{ID: "sambanova", Name: "SambaNova", Model: cfg.SambaNovaModel, Role: "fallback", Enabled: cfg.SambaNovaEnabled && cfg.SambaNovaAPIKey != "", Configured: cfg.SambaNovaEnabled && cfg.SambaNovaAPIKey != ""},
		{ID: "cerebras", Name: "Cerebras", Model: cfg.CerebrasModel, Role: "fallback", Enabled: cfg.CerebrasEnabled && cfg.CerebrasAPIKey != "", Configured: cfg.CerebrasEnabled && cfg.CerebrasAPIKey != ""},
		{ID: "ollama", Name: "Ollama", Model: cfg.OllamaModel, Role: "local", Enabled: cfg.OllamaFallback, Configured: cfg.OllamaFallback},
	}}
}

func cloneSettings(settings api.Settings) api.Settings {
	return api.Settings{Providers: append([]api.ProviderSetting(nil), settings.Providers...)}
}

func hasEnabledConfiguredProvider(settings api.Settings) bool {
	for _, provider := range settings.Providers {
		if provider.Enabled && provider.Configured {
			return true
		}
	}
	return false
}

type routingAssistant struct {
	settings *providerSettingsStore
	clients  map[string]llm.Streamer
}

func newRoutingAssistant(cfg config.Config, settings *providerSettingsStore) *routingAssistant {
	providerCooldown := 10 * time.Minute
	return &routingAssistant{
		settings: settings,
		clients: map[string]llm.Streamer{
			"gemini":    llm.NewCooldownClient("gemini", llm.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel), providerCooldown),
			"sambanova": llm.NewCooldownClient("sambanova", llm.NewOpenAICompatibleClient("sambanova", cfg.SambaNovaBaseURL, cfg.SambaNovaAPIKey, cfg.SambaNovaModel), providerCooldown),
			"cerebras":  llm.NewCooldownClient("cerebras", llm.NewOpenAICompatibleClient("cerebras", cfg.CerebrasBaseURL, cfg.CerebrasAPIKey, cfg.CerebrasModel), providerCooldown),
			"ollama":    llm.NewCooldownClient("ollama", llm.NewOllamaClient(cfg.OllamaBaseURL, cfg.OllamaModel), providerCooldown),
		},
	}
}

func (a *routingAssistant) GenerateStream(ctx context.Context, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error {
	var chain llm.Streamer
	needsImages := llm.HasImageAttachments(attachments)
	for _, provider := range a.settings.GetSettings().Providers {
		if !provider.Enabled || !provider.Configured {
			continue
		}
		if needsImages && !providerSupportsImages(provider.ID) {
			continue
		}
		client := a.clients[provider.ID]
		if client == nil {
			continue
		}
		if chain == nil {
			chain = client
			continue
		}
		chain = llm.NewFallbackClient(chain, client)
	}
	if chain == nil {
		return llm.ErrMissingAPIKey
	}
	return chain.GenerateStream(ctx, messages, attachments, searchResults, onChunk)
}

func providerSupportsImages(providerID string) bool {
	return providerID == "gemini"
}

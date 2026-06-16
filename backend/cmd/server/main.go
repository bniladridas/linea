package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	"linea/backend/internal/daemon"
	"linea/backend/internal/doctor"
	"linea/backend/internal/llm"
	"linea/backend/internal/migrate"
	"linea/backend/internal/oauth"
	"linea/backend/internal/saas"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
	"linea/backend/internal/tui"
	"linea/backend/internal/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

var version = "dev"

var subcommandArgs []string

func main() {
	if len(os.Args) < 2 {
		runServer()
		return
	}

	// Support old-style -flag args for backward compatibility.
	cmd := os.Args[1]
	if len(cmd) > 0 && cmd[0] == '-' {
		cmd = strings.TrimLeft(cmd, "-")
	}

	// Remaining args after the subcommand.
	subcommandArgs = os.Args[2:]

	switch cmd {
	case "server":
		runServer()
	case "daemon":
		runDaemonCmd()
	case "install":
		runInstallCmd()
	case "uninstall":
		runUninstallCmd()
	case "status":
		runStatusCmd()
	case "tui":
		runTUICmd()
	case "tui-beta":
		runTUIBetaCmd()
	case "migrate":
		runMigrateCmd()
	case "check":
		runCheckCmd()
	case "check-server":
		runCheckServerCmd()
	case "agent-status":
		runAgentStatusCmd()
	case "version":
		fmt.Printf("linea %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		fmt.Fprintf(os.Stderr, "run 'linea help' for usage\n")
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Fprintf(os.Stderr, `Usage: linea <subcommand> [options]

Subcommands:
  server        start the web server (default)
  daemon        start the server as a background daemon
  install       install Linea as a LaunchAgent and start it
  uninstall     uninstall the Linea LaunchAgent and stop it
  status        show daemon status
  tui           run the terminal chat interface
  tui-beta      run the hand-rolled terminal chat interface
  migrate       apply database migrations
  check         run non-interactive health checks
  check-server  check a running Linea server URL
  agent-status  print local agent status
  version       print version
  help          show this help
`)
}

func loadEnvAndConfig() config.Config {
	if err := config.LoadEnvFile(); err != nil {
		slog.Error("load env file", "error", err)
		os.Exit(1)
	}
	return config.Load()
}

func runServer() {
	cfg := loadEnvAndConfig()
	ctx := context.Background()

	appStore := buildStore(ctx, cfg)
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

	oauthProviders := map[oauth.Provider]string{}
	oauthSecrets := map[oauth.Provider]string{}
	if cfg.GitHubClientID != "" {
		oauthProviders[oauth.ProviderGitHub] = cfg.GitHubClientID
		oauthSecrets[oauth.ProviderGitHub] = cfg.GitHubClientSecret
	}
	if cfg.GitLabClientID != "" {
		oauthProviders[oauth.ProviderGitLab] = cfg.GitLabClientID
		oauthSecrets[oauth.ProviderGitLab] = cfg.GitLabClientSecret
	}
	if cfg.GoogleClientID != "" {
		oauthProviders[oauth.ProviderGoogle] = cfg.GoogleClientID
		oauthSecrets[oauth.ProviderGoogle] = cfg.GoogleClientSecret
	}
	oauthCallbackURL := "http://" + cfg.APIAddr
	if host, _, err := net.SplitHostPort(cfg.APIAddr); err == nil {
		if host == "" || host == "0.0.0.0" || host == "127.0.0.1" {
			oauthCallbackURL = "http://localhost:" + cfg.APIAddr[strings.LastIndex(cfg.APIAddr, ":")+1:]
		}
	}
	oauthRegistry := oauth.NewRegistry(oauthCallbackURL, cfg.OAuthEncryptionKey, oauthProviders, oauthSecrets)

	var tokenMu sync.Mutex

	tokenFn := func(provider string) (string, error) {
		tokenMu.Lock()
		defer tokenMu.Unlock()

		tokens, err := appStore.ListOAuthTokens(ctx)
		if err != nil {
			return "", err
		}
		for _, tok := range tokens {
			if tok.Provider == provider {
				// Refresh if the token is expired and a refresh token exists
				if !tok.ExpiresAt.IsZero() && tok.ExpiresAt.Before(time.Now().UTC()) && len(tok.RefreshToken) > 0 {
					newAccess, newRefresh, newExpiry, err := oauthRegistry.RefreshToken(ctx, oauth.Provider(provider), tok.RefreshToken)
					if err != nil {
						slog.Warn("token refresh failed, using stored token", "provider", provider, "error", err)
					} else {
						tok.AccessToken = newAccess
						if !newExpiry.IsZero() {
							tok.ExpiresAt = newExpiry
						}
						if len(newRefresh) > 0 {
							tok.RefreshToken = newRefresh
						}
						if err := appStore.SaveOAuthToken(ctx, tok); err != nil {
							slog.Warn("token refresh: failed to save updated token", "error", err)
						}
					}
				}
				raw, err := oauthRegistry.DecryptToken(tok.AccessToken)
				if err != nil {
					return "", err
				}
				return raw, nil
			}
		}
		return "", fmt.Errorf("%s not connected", provider)
	}

	agentRuntime := newAgentRuntime(cfg, llmEditPlanner{assistant: llmClient}, agent.WithIntegrationServer(agent.NewIntegrationServer(tokenFn)))
	agentRuntime.SetBackgroundJobStorer(appStore)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := agentRuntime.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown agent runtime", "error", err)
		}
	}()

	apiServer := api.NewServerWithAgentRuntime(appStore, llmClient, newSearchClient(cfg), staticFiles, cfg.WebOrigin, version, func(ctx context.Context) api.Status { return appStatus(ctx, cfg, settingsStore.GetSettings()) }, settingsStore, agentRuntime)
	apiServer.SetOAuthRegistry(oauthRegistry)

	if cfg.LineaSaasMode {
		apiServer.EnableSaaS()
		apiServer.EnableAPIRoutes()
		slog.Info("saas mode enabled")
	} else if cfg.EnableAPI {
		if cfg.APIKey == "" {
			slog.Warn("LINEA_ENABLE_API is set but LINEA_API_KEY is empty; server continuing without /api/v1/* routes")
		} else {
			apiServer.EnableAPI(cfg.APIKey)
		}
	}
	if cfg.EnableAccounts {
		if cfg.OAuthEncryptionKey == "" {
			slog.Warn("LINEA_ENABLE_ACCOUNTS: set LINEA_OAUTH_ENCRYPTION_KEY if using OAuth provider tools with accounts")
		}
		apiServer.EnableAccounts()
	}

	handler := apiServer.Handler()
	if cfg.LineaSaasMode {
		saasMgr := saas.NewManager(appStore)
		saasMgr.SetAdminKey(cfg.LineaSaasAdminKey)
		if cfg.LineaSaasAdminKey == "" {
			slog.Warn("saas mode active without LINEA_SAAS_ADMIN_KEY; admin endpoints are unprotected")
		}
		saasH := saas.NewHandler(saasMgr)
		saasMux := http.NewServeMux()
		saasH.Register(saasMux)
		adminHandler := saas.AdminMiddleware(saasMgr, saasMux)
		mux := http.NewServeMux()
		saasH.RegisterPublic(mux)
		mux.Handle("/", handler)
		handler = saas.Middleware(saasMgr, adminHandler, mux)
	}

	server := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if isPublicAPIAddr(cfg.APIAddr) {
			slog.Warn("linea api is listening on a non-loopback address; use only behind a trusted network, VPN, or reverse proxy", "addr", cfg.APIAddr)
		}
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
	}
	if err := appStore.Close(); err != nil {
		slog.Error("close store", "error", err)
	}
}

func buildStore(ctx context.Context, cfg config.Config) store.Store {
	var appStore store.Store
	if cfg.EnableSync && cfg.SyncURL != "" {
		local := buildLocalStore(ctx, cfg, true)
		remote, err := store.NewRemoteStore(cfg.SyncURL, cfg.SyncToken)
		if err != nil {
			slog.Error("sync remote store", "error", err)
			return local
		}
		appStore = store.NewSyncStore(local, remote)
		slog.Info("sync enabled", "remote", cfg.SyncURL)
	} else if cfg.SyncURL != "" {
		remote, err := store.NewRemoteStore(cfg.SyncURL, cfg.SyncToken)
		if err != nil {
			slog.Error("remote store", "error", err)
			appStore = store.NewMemoryStore()
		} else {
			appStore = remote
			slog.Info("using remote store", "url", cfg.SyncURL)
		}
	} else {
		appStore = buildLocalStore(ctx, cfg, false)
	}
	return appStore
}

func buildLocalStore(ctx context.Context, cfg config.Config, syncMode bool) store.Store {
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("connect postgres", "error", err)
			if syncMode {
				slog.Warn("sync: falling back to in-memory local store")
				return store.NewMemoryStore()
			}
			os.Exit(1)
		}
		if err := pool.Ping(ctx); err != nil {
			slog.Error("ping postgres", "error", err)
			if syncMode {
				pool.Close()
				slog.Warn("sync: falling back to in-memory local store")
				return store.NewMemoryStore()
			}
			os.Exit(1)
		}
		return store.NewPostgresStore(pool)
	}
	slog.Warn("DATABASE_URL not set; using in-memory storage")
	return store.NewMemoryStore()
}

func runDaemonCmd() {
	cfg := loadEnvAndConfig()
	if err := daemon.StartBackground(cfg.APIAddr); err != nil {
		slog.Error("daemon", "error", err)
		os.Exit(1)
	}
}

func runInstallCmd() {
	if err := daemon.Install(); err != nil {
		slog.Error("install", "error", err)
		os.Exit(1)
	}
	slog.Info("LaunchAgent installed and loaded")
}

func runUninstallCmd() {
	if err := daemon.Uninstall(); err != nil {
		slog.Error("uninstall", "error", err)
		os.Exit(1)
	}
}

func runStatusCmd() {
	if err := daemon.PrintStatus(os.Stdout); err != nil {
		slog.Error("status", "error", err)
		os.Exit(1)
	}
}

func runTUICmd() {
	cfg := loadEnvAndConfig()
	ctx := context.Background()
	appStore := buildStore(ctx, cfg)
	settingsStore, err := newProviderSettingsStore(config.DefaultSettingsFilePath(), defaultProviderSettings(cfg))
	if err != nil {
		slog.Error("load settings", "error", err)
		os.Exit(1)
	}
	llmClient := newRoutingAssistant(cfg, settingsStore)
	agentRuntime := newAgentRuntime(cfg, llmEditPlanner{assistant: llmClient})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := agentRuntime.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown agent runtime", "error", err)
		}
	}()
	if err := tui.New(appStore, llmClient, os.Stdin, os.Stdout).WithSearcher(newSearchClient(cfg)).WithAgentRuntime(agentRuntime).Run(ctx); err != nil {
		slog.Error("tui", "error", err)
		os.Exit(1)
	}
}

func runTUIBetaCmd() {
	cfg := loadEnvAndConfig()
	ctx := context.Background()
	appStore := buildStore(ctx, cfg)
	settingsStore, err := newProviderSettingsStore(config.DefaultSettingsFilePath(), defaultProviderSettings(cfg))
	if err != nil {
		slog.Error("load settings", "error", err)
		os.Exit(1)
	}
	llmClient := newRoutingAssistant(cfg, settingsStore)
	agentRuntime := newAgentRuntime(cfg, llmEditPlanner{assistant: llmClient})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := agentRuntime.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown agent runtime", "error", err)
		}
	}()
	if err := tui.New(appStore, llmClient, os.Stdin, os.Stdout).WithSearcher(newSearchClient(cfg)).WithAgentRuntime(agentRuntime).RunBeta(ctx); err != nil {
		slog.Error("hand-rolled tui", "error", err)
		os.Exit(1)
	}
}

func runMigrateCmd() {
	cfg := loadEnvAndConfig()
	ctx := context.Background()
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
}

func runCheckCmd() {
	cfg := loadEnvAndConfig()
	ctx := context.Background()
	checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	results := doctor.Run(checkCtx, cfg)
	doctor.Print(results)
	if doctor.HasFailure(results) {
		os.Exit(1)
	}
}

func runCheckServerCmd() {
	ctx := context.Background()
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var serverURL string
	if len(subcommandArgs) > 0 {
		serverURL = subcommandArgs[0]
	}
	if serverURL == "" {
		fmt.Fprintln(os.Stderr, "usage: linea check-server <url>")
		os.Exit(1)
	}
	results := doctor.CheckServer(checkCtx, serverURL)
	doctor.Print(results)
	if doctor.HasFailure(results) {
		os.Exit(1)
	}
}

func runAgentStatusCmd() {
	cfg := loadEnvAndConfig()
	ctx := context.Background()
	if err := printAgentStatus(ctx, cfg, os.Stdout); err != nil {
		slog.Error("agent status", "error", err)
		os.Exit(1)
	}
}

func printAgentStatus(ctx context.Context, cfg config.Config, out io.Writer) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(newAgentRuntime(cfg).Status(ctx))
}

func newAgentRuntime(cfg config.Config, planners ...any) *agent.Runtime {
	options := []func(*agent.Runtime){
		agent.WithWorkspaceRoot(cfg.AgentWorkspaceDir),
		agent.WithDeveloperMode(cfg.AgentDeveloperMode, cfg.AgentWorkspaceTrust),
		agent.WithSkillsDir(cfg.AgentSkillsDir),
		agent.WithMCPConfigPath(cfg.AgentMCPConfig),
		agent.WithCommandAllowlist(cfg.AgentCommandAllowlist),
	}
	for _, p := range planners {
		switch v := p.(type) {
		case agent.EditPlanner:
			if v != nil {
				options = append(options, agent.WithEditPlanner(v))
			}
		case func(*agent.Runtime):
			options = append(options, v)
		}
	}
	if cfg.AgentLSPCommand != "" {
		options = append(options, agent.WithLSPCommand(cfg.AgentLSPCommand))
	}
	runtime := agent.NewRuntime(
		cfg.AgentRulesPath,
		options...,
	)
	runtime.LoadAuditLog()
	if cfg.AgentUnrestricted {
		runtime.SetUnrestricted(true)
	}
	return runtime
}

func isPublicAPIAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			return true
		}
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return !ip.IsLoopback()
	}
	return !strings.EqualFold(host, "localhost")
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
	builder.WriteString("Content must be the complete replacement text for one allowed file. Empty files may be new files. Do not include markdown fences.\n\n")
	if isTempAppPlannerRequest(request) {
		builder.WriteString("This is a temporary React app preview. Return only path src/App.jsx. ")
		builder.WriteString("The content must be a complete React module for the local preview. ")
		builder.WriteString("JSX is allowed. Keep imports local and browser-safe. ")
		builder.WriteString("Use React from \"react\" for JSX and hooks. ")
		builder.WriteString("Do not import network resources or packages unless they are already available to the local bundle. ")
		builder.WriteString("For visual edit requests, preserve existing copy, labels, and casing unless the goal asks to change them. ")
		builder.WriteString("Preserve useful existing app behavior unless the goal asks to replace it.\n\n")
	}
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

func isTempAppPlannerRequest(request agent.EditPlanRequest) bool {
	if len(request.Files) != 1 {
		return false
	}
	return request.Files[0].Path == "src/App.jsx"
}

func stripPlannerJSON(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return firstPlannerJSONObject(value)
	}
	_, rest, ok := strings.Cut(value, "\n")
	if !ok {
		return value
	}
	index := strings.LastIndex(rest, "```")
	if index < 0 {
		return value
	}
	return firstPlannerJSONObject(strings.TrimSpace(rest[:index]))
}

func firstPlannerJSONObject(value string) string {
	start := strings.Index(value, "{")
	if start < 0 {
		return value
	}
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(value); index++ {
		char := value[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch char {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return value[start : index+1]
			}
		}
	}
	return value
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
		ollama = doctor.CheckOllamaLocalModel(statusCtx, cfg)
		cancel()
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

	openaiCompatSetting := settingsByID["openai-compatible"]
	openaiCompatConfigured := cfg.OpenAICompatibleEnabled && cfg.OpenAICompatibleBaseURL != "" && cfg.OpenAICompatibleModel != ""
	openaiCompatRouteEnabled := openaiCompatConfigured && openaiCompatSetting.Enabled

	mlxSetting := settingsByID["mlx"]
	mlxResult := doctor.Result{Status: doctor.Warn, Message: "disabled"}
	if cfg.MLXEnabled && mlxSetting.Enabled {
		statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		mlxResult = doctor.CheckMLXLocalModel(statusCtx, cfg)
		cancel()
	}
	mlxEnabled := cfg.MLXEnabled && mlxSetting.Enabled && mlxResult.Status == doctor.Pass
	mlxState := "off"
	mlxDetail := ""
	switch {
	case !cfg.MLXEnabled || !mlxSetting.Enabled:
		mlxState = "off"
		mlxResult.Message = "off"
	case mlxEnabled:
		mlxState = "ready"
		mlxDetail = mlxResult.Message
	default:
		mlxState = "off"
		mlxDetail = mlxResult.Message
	}

	vllmSetting := settingsByID["vllm"]
	vllmResult := doctor.Result{Status: doctor.Warn, Message: "disabled"}
	if cfg.VLLMEnabled && vllmSetting.Enabled {
		statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		vllmResult = doctor.CheckVLLMLocalModel(statusCtx, cfg)
		cancel()
	}
	vllmEnabled := cfg.VLLMEnabled && vllmSetting.Enabled && vllmResult.Status == doctor.Pass
	vllmState := "off"
	vllmDetail := ""
	switch {
	case !cfg.VLLMEnabled || !vllmSetting.Enabled:
		vllmState = "off"
		vllmResult.Message = "off"
	case vllmEnabled:
		vllmState = "ready"
		vllmDetail = vllmResult.Message
	default:
		vllmState = "off"
		vllmDetail = vllmResult.Message
	}

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
		"vllm": {
			Name:    "vLLM",
			Model:   cfg.VLLMModel,
			Enabled: vllmEnabled,
			Role:    "local",
			State:   vllmState,
			Message: vllmResult.Message,
			Detail:  vllmDetail,
		},
		"mlx": {
			Name:    "MLX",
			Model:   cfg.MLXModel,
			Enabled: mlxEnabled,
			Role:    "local",
			State:   mlxState,
			Message: mlxResult.Message,
			Detail:  mlxDetail,
		},
		"openai-compatible": configuredProviderStatus("OpenAI Compatible", cfg.OpenAICompatibleModel, "local", openaiCompatRouteEnabled, openaiCompatConfigured),
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
	storage := "PostgreSQL"
	switch {
	case cfg.EnableSync && cfg.SyncURL != "":
		if cfg.DatabaseURL == "" {
			storage = "Syncing (memory)"
		} else {
			storage = "Syncing"
		}
	case cfg.SyncURL != "":
		storage = "Remote"
	case cfg.DatabaseURL == "":
		storage = "Memory"
	}
	return api.Status{
		Storage:   storage,
		Search:    search.ProviderName(cfg.BraveSearchAPIKey, cfg.SearXNGURL),
		Providers: providerStatuses(ctx, cfg, settings),
	}
}

func newSearchClient(cfg config.Config) *search.Client {
	return search.NewClient(
		search.WithBraveAPIKey(cfg.BraveSearchAPIKey),
		search.WithSearXNGURL(cfg.SearXNGURL),
	)
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
		{ID: "vllm", Name: "vLLM", Model: cfg.VLLMModel, Role: "local", Enabled: cfg.VLLMEnabled, Configured: cfg.VLLMEnabled && cfg.VLLMBaseURL != ""},
		{ID: "mlx", Name: "MLX", Model: cfg.MLXModel, Role: "local", Enabled: cfg.MLXEnabled, Configured: cfg.MLXEnabled && cfg.MLXBaseURL != ""},
		{ID: "openai-compatible", Name: "OpenAI Compatible", Model: cfg.OpenAICompatibleModel, Role: "local", Enabled: cfg.OpenAICompatibleEnabled, Configured: cfg.OpenAICompatibleEnabled && cfg.OpenAICompatibleBaseURL != ""},
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
			"gemini":            llm.NewCooldownClient("gemini", llm.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel), providerCooldown),
			"sambanova":         llm.NewCooldownClient("sambanova", llm.NewOpenAICompatibleClient("sambanova", cfg.SambaNovaBaseURL, cfg.SambaNovaAPIKey, cfg.SambaNovaModel), providerCooldown),
			"cerebras":          llm.NewCooldownClient("cerebras", llm.NewOpenAICompatibleClient("cerebras", cfg.CerebrasBaseURL, cfg.CerebrasAPIKey, cfg.CerebrasModel), providerCooldown),
			"ollama":            llm.NewCooldownClient("ollama", llm.NewOllamaClient(cfg.OllamaBaseURL, cfg.OllamaModel), providerCooldown),
			"vllm":              llm.NewCooldownClient("vllm", llm.NewOpenAICompatibleClient("vllm", cfg.VLLMBaseURL, "", cfg.VLLMModel), providerCooldown),
			"mlx":               llm.NewCooldownClient("mlx", llm.NewOpenAICompatibleClient("mlx", cfg.MLXBaseURL, "", cfg.MLXModel), providerCooldown),
			"openai-compatible": llm.NewCooldownClient("openai-compatible", llm.NewOpenAICompatibleClient("openai-compatible", cfg.OpenAICompatibleBaseURL, cfg.OpenAICompatibleAPIKey, cfg.OpenAICompatibleModel), providerCooldown),
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

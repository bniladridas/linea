package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linea/backend/internal/api"
	"linea/backend/internal/config"
	"linea/backend/internal/doctor"
	"linea/backend/internal/llm"
	"linea/backend/internal/migrate"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
	"linea/backend/internal/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	runMigrations := flag.Bool("migrate", false, "apply database migrations and exit")
	runCheck := flag.Bool("check", false, "run non-interactive health checks and exit")
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
	providerCooldown := 10 * time.Minute
	llmClient := llm.Streamer(llm.NewCooldownClient("gemini", llm.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel), providerCooldown))
	if cfg.SambaNovaEnabled && cfg.SambaNovaAPIKey != "" {
		sambanova := llm.NewOpenAICompatibleClient("sambanova", cfg.SambaNovaBaseURL, cfg.SambaNovaAPIKey, cfg.SambaNovaModel)
		llmClient = llm.NewFallbackClient(llmClient, llm.NewCooldownClient("sambanova", sambanova, providerCooldown))
	}
	if cfg.CerebrasEnabled && cfg.CerebrasAPIKey != "" {
		cerebras := llm.NewOpenAICompatibleClient("cerebras", cfg.CerebrasBaseURL, cfg.CerebrasAPIKey, cfg.CerebrasModel)
		llmClient = llm.NewFallbackClient(llmClient, llm.NewCooldownClient("cerebras", cerebras, providerCooldown))
	}
	if cfg.OllamaFallback {
		llmClient = llm.NewFallbackClient(llmClient, llm.NewCooldownClient("ollama", llm.NewOllamaClient(cfg.OllamaBaseURL, cfg.OllamaModel), providerCooldown))
	}
	appStatus := api.Status{
		Storage: "PostgreSQL",
		Search:  "DuckDuckGo",
		Providers: []api.ProviderStatus{
			{Name: "Gemini", Model: cfg.GeminiModel, Enabled: cfg.GeminiAPIKey != "", Role: "primary"},
			{Name: "SambaNova", Model: cfg.SambaNovaModel, Enabled: cfg.SambaNovaEnabled && cfg.SambaNovaAPIKey != "", Role: "fallback"},
			{Name: "Cerebras", Model: cfg.CerebrasModel, Enabled: cfg.CerebrasEnabled && cfg.CerebrasAPIKey != "", Role: "fallback"},
			{Name: "Ollama", Model: cfg.OllamaModel, Enabled: cfg.OllamaFallback, Role: "local"},
		},
	}
	if cfg.DatabaseURL == "" {
		appStatus.Storage = "Memory"
	}

	server := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           api.NewServer(appStore, llmClient, search.NewClient(), staticFiles, cfg.WebOrigin, appStatus).Handler(),
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

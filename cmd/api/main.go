package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/ai"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/cache"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/platform/llm"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
	"github.com/pengfeifei6/NexusDesk/internal/platform/ratelimit"
	"github.com/pengfeifei6/NexusDesk/internal/platform/storage"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	registry := observe.NewRegistry()
	pool, err := database.Open(ctx, cfg.Database, observe.NewQueryTracer(registry))
	if err != nil {
		return err
	}
	defer pool.Close()
	registry.SetPool(pool)
	slog.Info("PostgreSQL connection verified")
	redisClient := cache.NewClient(cfg.Async, cfg.Cache.Timeout)
	defer redisClient.Close()
	var listCache *cache.Cache
	if cfg.Cache.Enabled {
		listCache = cache.New(redisClient, pool, cache.Options{TTL: cfg.Cache.TTL, Timeout: cfg.Cache.Timeout, MaxBytes: cfg.Cache.MaxBytes})
		defer func() {
			stats := listCache.Stats()
			slog.Info("list cache stopped", "hits", stats.Hits, "misses", stats.Misses, "failures", stats.Failures, "corrupt", stats.Corrupt, "stores", stats.Stores)
		}()
	}
	if listCache != nil {
		registry.SetCacheStats(func() observe.CacheStats {
			s := listCache.Stats()
			return observe.CacheStats{Hits: s.Hits, Misses: s.Misses, Failures: s.Failures, Corrupt: s.Corrupt, Stores: s.Stores}
		})
	}
	diagnostics, err := observe.StartDiagnostics(ctx, cfg.ObservabilityAddr, registry)
	if err != nil {
		return err
	}
	defer diagnostics.Close()
	if diagnostics.Addr() != "" {
		slog.Info("diagnostics started", "addr", diagnostics.Addr())
	}
	users := auth.NewPostgresRepository(pool)
	passwords := auth.NewPasswordHasher()
	registration := auth.NewHandler(auth.NewService(users, passwords))
	tokens, err := auth.NewTokenManager(auth.TokenConfig{Issuer: cfg.JWT.Issuer, Audience: cfg.JWT.Audience, TTL: cfg.JWT.TTL, Secret: cfg.JWT.Secret})
	if err != nil {
		return err
	}
	loginService, err := auth.NewLoginService(ctx, users, passwords, tokens)
	if err != nil {
		return err
	}
	login := auth.NewLoginHandler(loginService)
	me := auth.NewMeHandler(users)
	organizations := organization.NewHandler(organization.NewService(organization.NewPostgresRepository(pool)))
	objects, err := storage.NewLocal(cfg.Storage.Root)
	if err != nil {
		return errors.New("initialize private storage: check STORAGE_ROOT and directory permissions")
	}
	defer objects.Close()
	knowledgeService := knowledge.NewService(knowledge.NewPostgresRepository(pool), organization.NewPostgresRepository(pool), objects,
		knowledge.NewTextExtractor(cfg.Storage.MaxFileBytes, cfg.Storage.MaxTextBytes),
		knowledge.Limits{MaxFileBytes: cfg.Storage.MaxFileBytes, MaxRequestBytes: cfg.Storage.MaxRequestBytes, ProcessTimeout: cfg.Storage.ExtractTimeout}).WithCache(listCache)
	models, err := llm.New(cfg.RAG.OllamaBaseURL)
	if err != nil {
		return err
	}
	generator := models.Generator()
	streamModel := models.StreamModel()
	if cfg.RAG.ChatProvider == "deepseek" {
		generator, err = llm.NewDeepSeek(cfg.RAG.DeepSeekAPIKey, cfg.RAG.ChatModel)
		if err != nil {
			return err
		}
		streamModel, err = llm.NewDeepSeekStream(cfg.RAG.DeepSeekAPIKey, cfg.RAG.ChatModel)
		if err != nil {
			return err
		}
	}
	slog.Info("AI configured", "chat_provider", cfg.RAG.ChatProvider, "chat_model", cfg.RAG.ChatModel, "embedding_model", rag.EmbeddingModel)
	ragService := rag.NewService(pool, knowledgeService, models.Embedder(), generator, rag.Limits{Enabled: cfg.RAG.Enabled, MinimumScore: cfg.RAG.MinimumScore})
	tickets := ticket.NewService(ticket.NewPostgresRepository(pool), organization.NewPostgresRepository(pool)).WithCache(listCache)
	aiStore := ai.NewStore(pool, tickets)
	aiChat := ai.NewChatService(aiStore, ai.NewRunner(streamModel, ragService, tickets, aiStore))
	return runHTTP(ctx, cfg.HTTPAddr, httpserver.NewRouterWithObserver(registry, slog.Default(), registration.RegisterRoutes, login.RegisterRoutes, func(group *gin.RouterGroup) {
		protected := group.Group("")
		protected.Use(auth.RequireUser(tokens, users))
		me.RegisterRoutes(protected)
		organizations.RegisterRoutes(protected)
		organization.NewMemberHandler(organization.NewMemberService(organization.NewPostgresRepository(pool))).RegisterRoutes(protected)
		ticket.NewHandler(tickets).RegisterRoutes(protected)
		knowledge.NewHandler(knowledgeService).RegisterRoutes(protected)
		rag.NewHandler(ragService).RegisterRoutes(protected)
		jobs.NewHandler(jobs.NewService(pool, knowledgeService, ragService, jobs.Limits{Enabled: cfg.Async.Enabled})).RegisterRoutes(protected)
		aiHandler := ai.NewHandler(aiChat)
		if cfg.AIRate.Enabled {
			aiHandler.WithLimiter(ratelimit.New(redisClient, cfg.AIRate.Requests, cfg.AIRate.Window, cfg.Cache.Timeout))
		}
		aiHandler.RegisterRoutes(protected)
	}))
}

func runHTTP(ctx context.Context, addr string, handler http.Handler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen HTTP server: %w", err)
	}
	defer listener.Close()

	server := &http.Server{
		BaseContext:       func(net.Listener) context.Context { return ctx },
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      75 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	slog.Info("HTTP server started", "addr", listener.Addr().String())

	select {
	case err := <-serveResult:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		// The application context is already canceled, so shutdown needs its own.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if err := <-serveResult; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		slog.Info("HTTP server stopped")
		return nil
	}
}

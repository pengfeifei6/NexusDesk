package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/llm"
	"github.com/pengfeifei6/NexusDesk/internal/platform/storage"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskqueue"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		slog.Error("Worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return err
	}
	if !cfg.Async.Enabled {
		return errors.New("ASYNC_ENABLED must be true to start Worker")
	}
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	objects, err := storage.NewLocal(cfg.Storage.Root)
	if err != nil {
		return errors.New("initialize private storage: check STORAGE_ROOT and directory permissions")
	}
	defer objects.Close()
	documents := knowledge.NewService(knowledge.NewPostgresRepository(pool), organization.NewPostgresRepository(pool), objects, knowledge.NewTextExtractor(cfg.Storage.MaxFileBytes, cfg.Storage.MaxTextBytes), knowledge.Limits{MaxFileBytes: cfg.Storage.MaxFileBytes, MaxRequestBytes: cfg.Storage.MaxRequestBytes, ProcessTimeout: cfg.Storage.ExtractTimeout})
	models, err := llm.New(cfg.RAG.OllamaBaseURL)
	if err != nil {
		return err
	}
	indexer := rag.NewService(pool, documents, models.Embedder(), models.Generator(), rag.Limits{Enabled: cfg.RAG.Enabled, MinimumScore: cfg.RAG.MinimumScore, IndexTimeout: 150 * time.Second})
	service := jobs.NewService(pool, documents, indexer, jobs.Limits{Enabled: cfg.Async.Enabled})
	queue := taskqueue.NewClient(cfg.Async)
	defer queue.Close()
	if err = queue.Ping(ctx); err != nil {
		return errors.New("Redis unavailable: check local queue configuration")
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := taskqueue.NewServer(workerCtx, cfg.Async, service.Execute)
	if err = server.Start(); err != nil {
		return err
	}
	defer server.Shutdown()
	slog.Info("Worker started", "concurrency", cfg.Async.Concurrency)
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		if err := service.Dispatch(workerCtx, queue); err != nil && workerCtx.Err() == nil {
			slog.Warn("job dispatch pass failed")
		}
		select {
		case <-ctx.Done():
			server.Stop()
			cancel()
			slog.Info("Worker stopping")
			return nil
		case <-timer.C:
		}
	}
}

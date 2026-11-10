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

	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func main() {
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
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("PostgreSQL connection verified")
	return runHTTP(ctx, cfg.HTTPAddr)
}

func runHTTP(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen HTTP server: %w", err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           httpserver.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
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

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
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
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
	return runHTTP(ctx, cfg.HTTPAddr, httpserver.NewRouter(registration.RegisterRoutes, login.RegisterRoutes, func(group *gin.RouterGroup) {
		protected := group.Group("")
		protected.Use(auth.RequireUser(tokens, users))
		me.RegisterRoutes(protected)
		organizations.RegisterRoutes(protected)
		organization.NewMemberHandler(organization.NewMemberService(organization.NewPostgresRepository(pool))).RegisterRoutes(protected)
		ticket.NewHandler(ticket.NewService(ticket.NewPostgresRepository(pool), organization.NewPostgresRepository(pool))).RegisterRoutes(protected)
	}))
}

func runHTTP(ctx context.Context, addr string, handler http.Handler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen HTTP server: %w", err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
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

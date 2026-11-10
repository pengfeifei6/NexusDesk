package database

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
)

func testDatabaseConfig() config.DatabaseConfig {
	return config.DatabaseConfig{
		Host: "127.0.0.1", Port: 5432, User: "nexusdesk", Name: "nexusdesk",
		Password: "test-only ' \\ password", SSLMode: "disable",
	}
}

func TestOpenRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := Open(ctx, testDatabaseConfig())
	if pool != nil {
		pool.Close()
		t.Fatal("Open returned a pool for a canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestOpenRejectsUnavailableDatabaseWithoutLeakingPassword(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	cfg := testDatabaseConfig()
	cfg.Port, _ = strconv.Atoi(port)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool, err := Open(ctx, cfg)
	if pool != nil {
		pool.Close()
		t.Fatal("Open returned a pool without a reachable database")
	}
	if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
		t.Fatalf("expected a database connection error, got %v", err)
	}
	if strings.Contains(err.Error(), cfg.Password) || strings.Contains(err.Error(), "postgres://") {
		t.Fatal("connection error exposed credentials")
	}
}

func TestOpenRejectsInvalidSSLModeWithoutLeakingPassword(t *testing.T) {
	cfg := testDatabaseConfig()
	cfg.SSLMode = "not-an-ssl-mode"
	pool, err := Open(context.Background(), cfg)
	if pool != nil {
		pool.Close()
		t.Fatal("Open accepted invalid connection settings")
	}
	if err == nil || strings.Contains(err.Error(), cfg.Password) {
		t.Fatalf("expected a safe configuration error, got %v", err)
	}
}

// Accept TCP but never answer PostgreSQL's startup packet.
func blackholeDatabase(t *testing.T) (config.DatabaseConfig, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	finished := make(chan struct{})
	t.Cleanup(func() {
		listener.Close()
		close(finished)
	})
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		close(accepted)
		<-finished
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	cfg := testDatabaseConfig()
	cfg.Port, _ = strconv.Atoi(port)
	return cfg, accepted
}

func TestOpenBoundsUnresponsiveDatabaseStartup(t *testing.T) {
	cfg, _ := blackholeDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	started := time.Now()
	pool, err := Open(ctx, cfg)
	if pool != nil {
		pool.Close()
		t.Fatal("Open accepted a database that never answered")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected startup deadline, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 7*time.Second {
		t.Fatalf("startup exceeded the five-second budget and scheduling tolerance: %v", elapsed)
	}
}

func TestOpenCancelsPendingConnection(t *testing.T) {
	cfg, accepted := blackholeDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		pool, err := Open(ctx, cfg)
		if pool != nil {
			pool.Close()
		}
		result <- err
	}()
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not reach the test server")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected in-flight cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Open ignored cancellation during the connection handshake")
	}
}

// This test is selected by scripts/day5-smoke.py against its disposable database.
func TestOpenPostgresIntegration(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_POSTGRES") != "1" {
		t.Skip("requires the disposable database created by scripts/day5-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var result int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil || result != 1 {
		t.Fatalf("database query failed: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var id int64
	var created time.Time
	err = tx.QueryRow(ctx, "INSERT INTO organizations (name) VALUES ($1) RETURNING id, created_at", "Day 5 test").Scan(&id, &created)
	if err != nil || id <= 0 || created.IsZero() {
		t.Fatalf("identity or timestamp defaults failed: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

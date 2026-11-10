package main

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func setAPIEnvironment(t *testing.T, addr string) {
	t.Helper()
	for key, value := range map[string]string{
		"APP_ENV": "test", "HTTP_ADDR": addr,
		"DB_HOST": "127.0.0.1", "DB_PORT": "5432", "DB_USER": "nexusdesk",
		"DB_PASSWORD": "test-only-password", "DB_NAME": "nexusdesk", "DB_SSLMODE": "disable",
		"JWT_ISSUER": "nexusdesk", "JWT_AUDIENCE": "nexusdesk-api", "JWT_TTL_MINUTES": "15",
		"JWT_SECRET_BASE64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("J", 32))),
	} {
		t.Setenv(key, value)
	}
}

func TestRunRejectsMissingConfiguration(t *testing.T) {
	setAPIEnvironment(t, "127.0.0.1:0")
	t.Setenv("HTTP_ADDR", "")
	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR") {
		t.Fatalf("expected configuration error, got %v", err)
	}
}

func TestRunRejectsUnavailableDatabase(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	setAPIEnvironment(t, "127.0.0.1:0")
	t.Setenv("DB_PORT", port)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := run(ctx); err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
		t.Fatalf("expected a database startup error, got %v", err)
	}
}

func TestRunReportsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := runHTTP(context.Background(), listener.Addr().String(), httpserver.NewRouter()); err == nil || !strings.Contains(err.Error(), "listen HTTP server") {
		t.Fatalf("expected listener error for the occupied port, got %v", err)
	}
}

func TestRunStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runHTTP(ctx, "127.0.0.1:0", httpserver.NewRouter()) }()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("cancellation returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
}

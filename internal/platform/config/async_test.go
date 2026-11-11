package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func clearAsyncEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ASYNC_ENABLED", "REDIS_ADDR", "REDIS_DB", "REDIS_PASSWORD", "WORKER_CONCURRENCY"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkerConfigurationDoesNotRequireHTTPOrJWT(t *testing.T) {
	clearAsyncEnvironment(t)
	setValidEnvironment(t)
	for _, key := range []string{"HTTP_ADDR", "JWT_ISSUER", "JWT_AUDIENCE", "JWT_TTL_MINUTES", "JWT_SECRET_BASE64"} {
		t.Setenv(key, "")
	}
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Port != 5432 || !cfg.Async.Enabled || cfg.Async.RedisAddr != "127.0.0.1:6380" || cfg.Async.RedisDB != 6 || cfg.Async.Concurrency != 2 {
		t.Fatal("worker defaults incorrect")
	}
}

func TestAsyncConfigurationRejectsUnsafeOrUnboundedValues(t *testing.T) {
	clearAsyncEnvironment(t)
	for _, tc := range []struct{ key, value string }{
		{"ASYNC_ENABLED", "1"}, {"REDIS_ADDR", "redis://secret@127.0.0.1:6380"}, {"REDIS_ADDR", "localhost:6379"}, {"REDIS_ADDR", "0.0.0.0:6379"}, {"REDIS_ADDR", "127.0.0.1:0"}, {"REDIS_DB", "16"}, {"REDIS_DB", "-1"}, {"WORKER_CONCURRENCY", "0"}, {"WORKER_CONCURRENCY", "5"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("want field error, got %v", err)
			}
		})
	}
}

func TestWorkerDatabaseValidationAndSecretOmission(t *testing.T) {
	clearAsyncEnvironment(t)
	setValidEnvironment(t)
	t.Setenv("REDIS_PASSWORD", "test-only-redis-secret")
	t.Setenv("ASYNC_ENABLED", "false")
	t.Setenv("REDIS_ADDR", "[::1]:6379")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg.Async)
	if cfg.Async.Enabled || cfg.Async.RedisPassword != "test-only-redis-secret" || strings.Contains(string(b), "secret") {
		t.Fatal("async configuration incorrect or exposed secret")
	}
	t.Setenv("DB_PASSWORD", "REPLACE_WITH_LOCAL_PASSWORD")
	if _, err := LoadWorker(); err == nil {
		t.Fatal("worker bypassed database validation")
	}
}

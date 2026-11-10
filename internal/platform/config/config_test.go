package config

import (
	"strings"
	"testing"
)

func setValidEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"APP_ENV": "development", "HTTP_ADDR": "127.0.0.1:8080",
		"DB_HOST": "127.0.0.1", "DB_PORT": "5432", "DB_USER": "nexusdesk",
		"DB_PASSWORD": " test-only password ", "DB_NAME": "nexusdesk", "DB_SSLMODE": "disable",
	} {
		t.Setenv(key, value)
	}
}

func TestLoadValidEnvironment(t *testing.T) {
	setValidEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AppEnv != "development" || cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatal("application configuration did not match environment")
	}
	if cfg.Database.Host != "127.0.0.1" || cfg.Database.Port != 5432 ||
		cfg.Database.User != "nexusdesk" || cfg.Database.Name != "nexusdesk" || cfg.Database.SSLMode != "disable" {
		t.Fatal("database configuration did not match environment")
	}
	if cfg.Database.Password != " test-only password " {
		t.Fatal("password contents were changed")
	}
}

func TestLoadRejectsMissingVariables(t *testing.T) {
	for _, key := range []string{"APP_ENV", "HTTP_ADDR", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE"} {
		t.Run(key, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(key, "")
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("expected error naming %s, got %v", key, err)
			}
			if strings.Contains(err.Error(), "test-only password") {
				t.Fatal("configuration error exposed the password")
			}
		})
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"blank environment", "APP_ENV", " \t"},
		{"missing HTTP port", "HTTP_ADDR", "127.0.0.1"},
		{"named HTTP port", "HTTP_ADDR", "127.0.0.1:http"},
		{"negative HTTP port", "HTTP_ADDR", "127.0.0.1:-1"},
		{"HTTP port too large", "HTTP_ADDR", "127.0.0.1:65536"},
		{"invalid DB port", "DB_PORT", "not-a-port"},
		{"zero DB port", "DB_PORT", "0"},
		{"DB port too large", "DB_PORT", "65536"},
		{"invalid SSL mode", "DB_SSLMODE", "unknown"},
		{"blank password", "DB_PASSWORD", " \t"},
		{"placeholder password", "DB_PASSWORD", "REPLACE_WITH_LOCAL_PASSWORD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected error naming %s, got %v", tc.key, err)
			}
			if strings.Contains(err.Error(), "test-only password") || strings.Contains(err.Error(), tc.value) && tc.key == "DB_PASSWORD" {
				t.Fatal("configuration error exposed a password value")
			}
		})
	}
}

func TestLoadAllowsIPv6AndEphemeralHTTPPort(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("HTTP_ADDR", "[::1]:0")
	if _, err := Load(); err != nil {
		t.Fatalf("expected IPv6 and port 0 to be accepted: %v", err)
	}
}

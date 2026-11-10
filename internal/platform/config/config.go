// Package config reads application settings from process environment variables.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Config contains the settings needed to start the API.
type Config struct {
	AppEnv   string
	HTTPAddr string
	Database DatabaseConfig
	JWT      JWTConfig
}

type JWTConfig struct {
	Issuer   string
	Audience string
	TTL      time.Duration
	Secret   []byte `json:"-"`
}

// DatabaseConfig provides the connection settings for PostgreSQL.
// Do not log this struct: Password contains a credential.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// Load reads and validates the environment. It does not read a .env file.
func Load() (Config, error) {
	var cfg Config
	var dbPort string
	var jwtTTL, jwtSecret string
	variables := []struct {
		name   string
		target *string
	}{
		{"APP_ENV", &cfg.AppEnv},
		{"HTTP_ADDR", &cfg.HTTPAddr},
		{"DB_HOST", &cfg.Database.Host},
		{"DB_PORT", &dbPort},
		{"DB_USER", &cfg.Database.User},
		{"DB_PASSWORD", &cfg.Database.Password},
		{"DB_NAME", &cfg.Database.Name},
		{"DB_SSLMODE", &cfg.Database.SSLMode},
		{"JWT_ISSUER", &cfg.JWT.Issuer},
		{"JWT_AUDIENCE", &cfg.JWT.Audience},
		{"JWT_TTL_MINUTES", &jwtTTL},
		{"JWT_SECRET_BASE64", &jwtSecret},
	}
	for _, variable := range variables {
		value := os.Getenv(variable.name)
		if strings.TrimSpace(value) == "" {
			return Config{}, fmt.Errorf("%s is required", variable.name)
		}
		if variable.name != "DB_PASSWORD" && variable.name != "JWT_SECRET_BASE64" {
			value = strings.TrimSpace(value)
		}
		*variable.target = value
	}

	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR must use host:port format")
	}
	httpPort, err := strconv.Atoi(port)
	if err != nil || httpPort < 0 || httpPort > 65535 {
		return Config{}, fmt.Errorf("HTTP_ADDR must contain a numeric port between 0 and 65535")
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return Config{}, fmt.Errorf("HTTP_ADDR must use an explicit loopback IP for local authentication")
	}
	cfg.Database.Port, err = strconv.Atoi(dbPort)
	if err != nil || cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
		return Config{}, fmt.Errorf("DB_PORT must be an integer between 1 and 65535")
	}
	if cfg.Database.Password == "REPLACE_WITH_LOCAL_PASSWORD" {
		return Config{}, fmt.Errorf("DB_PASSWORD must be configured locally")
	}
	switch cfg.Database.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return Config{}, fmt.Errorf("DB_SSLMODE is not a supported PostgreSQL SSL mode")
	}
	for _, field := range []struct{ name, value string }{{"JWT_ISSUER", cfg.JWT.Issuer}, {"JWT_AUDIENCE", cfg.JWT.Audience}} {
		if !utf8.ValidString(field.value) || len(field.value) > 256 {
			return Config{}, fmt.Errorf("%s must be valid UTF-8 and at most 256 bytes", field.name)
		}
	}
	minutes, err := strconv.Atoi(jwtTTL)
	if err != nil || minutes != 15 {
		return Config{}, fmt.Errorf("JWT_TTL_MINUTES must be 15 for this release")
	}
	cfg.JWT.TTL = 15 * time.Minute
	cfg.JWT.Secret, err = base64.StdEncoding.Strict().DecodeString(jwtSecret)
	if err != nil || len(cfg.JWT.Secret) < 32 || base64.StdEncoding.EncodeToString(cfg.JWT.Secret) != jwtSecret {
		return Config{}, fmt.Errorf("JWT_SECRET_BASE64 must be canonical standard Base64 encoding of at least 32 bytes")
	}
	return cfg, nil
}

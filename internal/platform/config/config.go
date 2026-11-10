// Package config reads application settings from process environment variables.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config contains the settings needed to start the API.
type Config struct {
	AppEnv   string
	HTTPAddr string
	Database DatabaseConfig
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
	}
	for _, variable := range variables {
		value := os.Getenv(variable.name)
		if strings.TrimSpace(value) == "" {
			return Config{}, fmt.Errorf("%s is required", variable.name)
		}
		if variable.name != "DB_PASSWORD" {
			value = strings.TrimSpace(value)
		}
		*variable.target = value
	}

	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR must use host:port format")
	}
	httpPort, err := strconv.Atoi(port)
	if err != nil || httpPort < 0 || httpPort > 65535 {
		return Config{}, fmt.Errorf("HTTP_ADDR must contain a numeric port between 0 and 65535")
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
	return cfg, nil
}

package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type AsyncConfig struct {
	Enabled       bool
	RedisAddr     string
	RedisDB       int
	RedisPassword string `json:"-"`
	Concurrency   int
}

func loadAsync() (AsyncConfig, error) {
	cfg := AsyncConfig{Enabled: true, RedisAddr: "127.0.0.1:6380", RedisDB: 6, Concurrency: 2, RedisPassword: os.Getenv("REDIS_PASSWORD")}
	if v, ok := os.LookupEnv("ASYNC_ENABLED"); ok {
		if v != "true" && v != "false" {
			return AsyncConfig{}, fmt.Errorf("ASYNC_ENABLED must be true or false")
		}
		cfg.Enabled = v == "true"
	}
	if v, ok := os.LookupEnv("REDIS_ADDR"); ok {
		cfg.RedisAddr = v
	}
	host, port, err := net.SplitHostPort(cfg.RedisAddr)
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || e != nil || n < 1 || n > 65535 {
		return AsyncConfig{}, fmt.Errorf("REDIS_ADDR must use a literal loopback IP and port")
	}
	for _, f := range []struct {
		name     string
		target   *int
		min, max int
	}{{"REDIS_DB", &cfg.RedisDB, 0, 15}, {"WORKER_CONCURRENCY", &cfg.Concurrency, 1, 4}} {
		if v, ok := os.LookupEnv(f.name); ok {
			n, e := strconv.Atoi(v)
			if e != nil || n < f.min || n > f.max {
				return AsyncConfig{}, fmt.Errorf("%s is outside its supported range", f.name)
			}
			*f.target = n
		}
	}
	return cfg, nil
}

// WorkerConfig deliberately carries neither HTTP nor JWT signing configuration.
type WorkerConfig struct {
	AppEnv   string
	Database DatabaseConfig
	Storage  StorageConfig
	RAG      RAGConfig
	Async    AsyncConfig
}

func LoadWorker() (WorkerConfig, error) {
	cfg := WorkerConfig{AppEnv: strings.TrimSpace(os.Getenv("APP_ENV"))}
	if cfg.AppEnv == "" {
		return WorkerConfig{}, fmt.Errorf("APP_ENV is required")
	}
	var err error
	if cfg.Database, err = loadDatabase(); err != nil {
		return WorkerConfig{}, err
	}
	if cfg.Storage, err = loadStorage(); err != nil {
		return WorkerConfig{}, err
	}
	if cfg.RAG, err = loadRAG(); err != nil {
		return WorkerConfig{}, err
	}
	if cfg.Async, err = loadAsync(); err != nil {
		return WorkerConfig{}, err
	}
	return cfg, nil
}

func loadDatabase() (DatabaseConfig, error) {
	var cfg DatabaseConfig
	var port string
	for _, f := range []struct {
		name   string
		target *string
	}{{"DB_HOST", &cfg.Host}, {"DB_PORT", &port}, {"DB_USER", &cfg.User}, {"DB_PASSWORD", &cfg.Password}, {"DB_NAME", &cfg.Name}, {"DB_SSLMODE", &cfg.SSLMode}} {
		v := os.Getenv(f.name)
		if strings.TrimSpace(v) == "" {
			return DatabaseConfig{}, fmt.Errorf("%s is required", f.name)
		}
		if f.name != "DB_PASSWORD" {
			v = strings.TrimSpace(v)
		}
		*f.target = v
	}
	var err error
	cfg.Port, err = strconv.Atoi(port)
	if err != nil || cfg.Port < 1 || cfg.Port > 65535 {
		return DatabaseConfig{}, fmt.Errorf("DB_PORT must be an integer between 1 and 65535")
	}
	if cfg.Password == "REPLACE_WITH_LOCAL_PASSWORD" {
		return DatabaseConfig{}, fmt.Errorf("DB_PASSWORD must be configured locally")
	}
	switch cfg.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return DatabaseConfig{}, fmt.Errorf("DB_SSLMODE is not a supported PostgreSQL SSL mode")
	}
	return cfg, nil
}

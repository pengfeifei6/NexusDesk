package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type StorageConfig struct {
	Root                                        string
	MaxFileBytes, MaxRequestBytes, MaxTextBytes int64
	ExtractTimeout                              time.Duration
}

func loadStorage() (StorageConfig, error) {
	cfg := StorageConfig{Root: "./var/storage", MaxFileBytes: 5242880, MaxRequestBytes: 8388608, MaxTextBytes: 1048576, ExtractTimeout: 10 * time.Second}
	if v, ok := os.LookupEnv("STORAGE_ROOT"); ok {
		cfg.Root = v
	}
	if strings.TrimSpace(cfg.Root) != cfg.Root || cfg.Root == "" || strings.ContainsRune(cfg.Root, 0) || filepath.Clean(cfg.Root) == "." || filepath.Clean(cfg.Root) == string(filepath.Separator) {
		return StorageConfig{}, fmt.Errorf("STORAGE_ROOT must name a private directory")
	}
	seconds := int64(10)
	for _, f := range []struct {
		name  string
		value *int64
		max   int64
	}{{"UPLOAD_MAX_FILE_BYTES", &cfg.MaxFileBytes, 64 << 20}, {"UPLOAD_MAX_REQUEST_BYTES", &cfg.MaxRequestBytes, 65 << 20}, {"TEXT_EXTRACT_MAX_BYTES", &cfg.MaxTextBytes, 1 << 20}, {"TEXT_EXTRACT_TIMEOUT_SECONDS", &seconds, 10}} {
		if v, ok := os.LookupEnv(f.name); ok {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 || n > f.max {
				return StorageConfig{}, fmt.Errorf("%s must be a bounded positive integer", f.name)
			}
			*f.value = n
		}
	}
	if cfg.MaxRequestBytes < cfg.MaxFileBytes+65536 {
		return StorageConfig{}, fmt.Errorf("UPLOAD_MAX_REQUEST_BYTES must allow at least 65536 bytes of multipart overhead")
	}
	cfg.ExtractTimeout = time.Duration(seconds) * time.Second
	return cfg, nil
}

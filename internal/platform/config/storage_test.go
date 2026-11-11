package config

import (
	"testing"
	"time"
)

func TestStorageDefaultsAndValidation(t *testing.T) {
	setValidEnvironment(t)
	for k, v := range map[string]string{"STORAGE_ROOT": "./var/storage", "UPLOAD_MAX_FILE_BYTES": "5242880", "UPLOAD_MAX_REQUEST_BYTES": "8388608", "TEXT_EXTRACT_MAX_BYTES": "1048576", "TEXT_EXTRACT_TIMEOUT_SECONDS": "10"} {
		t.Setenv(k, v)
	}
	cfg, err := Load()
	if err != nil || cfg.Storage.MaxFileBytes != 5242880 || cfg.Storage.MaxRequestBytes != 8388608 || cfg.Storage.MaxTextBytes != 1048576 || cfg.Storage.ExtractTimeout != 10*time.Second {
		t.Fatal("storage configuration", err)
	}
	for _, tc := range []struct{ k, v string }{{"UPLOAD_MAX_FILE_BYTES", "0"}, {"UPLOAD_MAX_FILE_BYTES", "67108865"}, {"UPLOAD_MAX_REQUEST_BYTES", "100"}, {"TEXT_EXTRACT_MAX_BYTES", "0"}, {"TEXT_EXTRACT_MAX_BYTES", "1048577"}, {"TEXT_EXTRACT_TIMEOUT_SECONDS", "11"}, {"TEXT_EXTRACT_TIMEOUT_SECONDS", "1.5"}, {"STORAGE_ROOT", ""}, {"STORAGE_ROOT", "."}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			t.Setenv(tc.k, tc.v)
			if _, err := Load(); err == nil {
				t.Fatal("invalid storage config accepted")
			}
		})
	}
}

package config

import (
	"os"
	"testing"
)

func TestRAGDefaultsAndLocalValidation(t *testing.T) {
	for _, key := range []string{"RAG_ENABLED", "OLLAMA_BASE_URL", "EMBEDDING_MODEL", "EMBEDDING_DIM", "CHAT_MODEL", "RAG_MIN_SCORE"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	cfg, err := loadRAG()
	if err != nil || !cfg.Enabled || cfg.OllamaBaseURL != "http://127.0.0.1:11434" || cfg.MinimumScore != 0.4 {
		t.Fatal("incorrect defaults", err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RAG_ENABLED", "yes"}, {"OLLAMA_BASE_URL", "https://example.com"}, {"OLLAMA_BASE_URL", "http://localhost:11434"}, {"OLLAMA_BASE_URL", "http://127.0.0.1:11434/api"}, {"OLLAMA_BASE_URL", "http://user:pass@127.0.0.1:11434"}, {"OLLAMA_BASE_URL", "http://127.0.0.1:0"}, {"OLLAMA_BASE_URL", "http://127.0.0.1:11434?x=y"}, {"OLLAMA_BASE_URL", "http://127.0.0.1:11434#"},
		{"EMBEDDING_MODEL", "different"}, {"EMBEDDING_DIM", "1536"}, {"CHAT_MODEL", "qwen3.5:397b-cloud"}, {"RAG_MIN_SCORE", "NaN"}, {"RAG_MIN_SCORE", "Inf"}, {"RAG_MIN_SCORE", "-1"}, {"RAG_MIN_SCORE", "1.1"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := loadRAG(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	t.Setenv("RAG_ENABLED", "false")
	t.Setenv("OLLAMA_BASE_URL", "http://[::1]:11434")
	cfg, err = loadRAG()
	if err != nil || cfg.Enabled {
		t.Fatal("disabled or IPv6 config failed", err)
	}
}

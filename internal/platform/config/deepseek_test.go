package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeepSeekConfiguration(t *testing.T) {
	t.Setenv("CHAT_PROVIDER", "deepseek")
	t.Setenv("CHAT_MODEL", "deepseek-flash")
	t.Setenv("DEEPSEEK_API_KEY", "test-only-secret")
	cfg, err := loadRAG()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatProvider != "deepseek" || cfg.ChatModel != "deepseek-flash" || cfg.DeepSeekAPIKey != "test-only-secret" {
		t.Fatal("wrong provider config")
	}
	data, _ := json.Marshal(cfg)
	if strings.Contains(string(data), "test-only-secret") {
		t.Fatal("serialized key")
	}
	for _, tc := range []struct{ k, v string }{{"CHAT_PROVIDER", "unknown"}, {"CHAT_MODEL", "qwen3.5:35b"}, {"DEEPSEEK_API_KEY", ""}, {"DEEPSEEK_API_KEY", "secret\r\nHeader: injected"}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			t.Setenv(tc.k, tc.v)
			_, e := loadRAG()
			if e == nil {
				t.Fatal("accepted invalid config")
			}
			if strings.Contains(e.Error(), "secret") {
				t.Fatal("leaked key")
			}
		})
	}
}
func TestWorkerDoesNotRequireCloudKey(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("CHAT_PROVIDER", "deepseek")
	t.Setenv("CHAT_MODEL", "deepseek-flash")
	t.Setenv("DEEPSEEK_API_KEY", "")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RAG.DeepSeekAPIKey != "" || cfg.RAG.ChatModel != "qwen3.5:35b" {
		t.Fatal("worker depends on cloud generation")
	}
}

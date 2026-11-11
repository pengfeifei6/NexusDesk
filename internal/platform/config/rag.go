package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type RAGConfig struct {
	Enabled        bool
	OllamaBaseURL  string
	MinimumScore   float64
	ChatProvider   string
	ChatModel      string
	DeepSeekAPIKey string `json:"-"`
}

func loadRAG() (RAGConfig, error) { return loadRAGSettings(true) }

func loadRAGSettings(requireChat bool) (RAGConfig, error) {
	cfg := RAGConfig{Enabled: true, OllamaBaseURL: "http://127.0.0.1:11434", MinimumScore: 0.40, ChatProvider: "ollama", ChatModel: "qwen3.5:35b"}
	if v, ok := os.LookupEnv("RAG_ENABLED"); ok {
		if v != "true" && v != "false" {
			return RAGConfig{}, fmt.Errorf("RAG_ENABLED must be true or false")
		}
		cfg.Enabled = v == "true"
	}
	if v, ok := os.LookupEnv("OLLAMA_BASE_URL"); ok {
		cfg.OllamaBaseURL = v
	}
	u, err := url.Parse(cfg.OllamaBaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(cfg.OllamaBaseURL, "#") || (u.Path != "" && u.Path != "/") {
		return RAGConfig{}, fmt.Errorf("OLLAMA_BASE_URL must be a local HTTP origin")
	}
	ip := net.ParseIP(u.Hostname())
	port, e := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || e != nil || port < 1 || port > 65535 {
		return RAGConfig{}, fmt.Errorf("OLLAMA_BASE_URL must use a literal loopback IP and port")
	}
	cfg.OllamaBaseURL = strings.TrimSuffix(cfg.OllamaBaseURL, "/")
	for key, want := range map[string]string{"EMBEDDING_MODEL": "bge-m3:567m", "EMBEDDING_DIM": "1024"} {
		if value, ok := os.LookupEnv(key); ok && value != want {
			return RAGConfig{}, fmt.Errorf("%s does not match this release's fixed model contract", key)
		}
	}
	if requireChat {
		if v, ok := os.LookupEnv("CHAT_PROVIDER"); ok {
			cfg.ChatProvider = v
		}
		switch cfg.ChatProvider {
		case "ollama":
			if v, ok := os.LookupEnv("CHAT_MODEL"); ok && v != cfg.ChatModel {
				return RAGConfig{}, fmt.Errorf("CHAT_MODEL must be qwen3.5:35b for Ollama")
			}
		case "deepseek":
			cfg.ChatModel = "deepseek-flash"
			if v, ok := os.LookupEnv("CHAT_MODEL"); ok {
				cfg.ChatModel = v
			}
			if cfg.ChatModel != "deepseek-flash" && cfg.ChatModel != "deepseek-v4-pro" {
				return RAGConfig{}, fmt.Errorf("CHAT_MODEL must be a supported DeepSeek model")
			}
			cfg.DeepSeekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
			if len(cfg.DeepSeekAPIKey) == 0 || len(cfg.DeepSeekAPIKey) > 4096 || strings.IndexFunc(cfg.DeepSeekAPIKey, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
				return RAGConfig{}, fmt.Errorf("DEEPSEEK_API_KEY must be a nonempty ASCII credential without whitespace")
			}
		default:
			return RAGConfig{}, fmt.Errorf("CHAT_PROVIDER must be ollama or deepseek")
		}
	}

	if value, ok := os.LookupEnv("RAG_MIN_SCORE"); ok {
		score, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			return RAGConfig{}, fmt.Errorf("RAG_MIN_SCORE must be between 0 and 1")
		}
		cfg.MinimumScore = score
	}
	return cfg, nil
}

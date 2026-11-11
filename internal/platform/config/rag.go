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
	Enabled       bool
	OllamaBaseURL string
	MinimumScore  float64
}

func loadRAG() (RAGConfig, error) {
	cfg := RAGConfig{Enabled: true, OllamaBaseURL: "http://127.0.0.1:11434", MinimumScore: 0.40}
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
	for key, want := range map[string]string{"EMBEDDING_MODEL": "bge-m3:567m", "EMBEDDING_DIM": "1024", "CHAT_MODEL": "qwen3.5:35b"} {
		if value, ok := os.LookupEnv(key); ok && value != want {
			return RAGConfig{}, fmt.Errorf("%s does not match this release's fixed model contract", key)
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

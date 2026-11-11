package rag

import (
	"context"
	"math"
)

const (
	EmbeddingModel     = "bge-m3:567m"
	EmbeddingDimension = 1024
	ChatModel          = "qwen3.5:35b"
)

// SupportedChatModel preserves the configured generation allowlist independently of embedding indexes.
func SupportedChatModel(model string) bool {
	return model == ChatModel || model == "deepseek-flash" || model == "deepseek-v4-pro"
}

type Embedder interface {
	Model() string
	Dimension() int
	Embed(context.Context, []string) ([][]float32, error)
}

type Generator interface {
	Model() string
	Generate(context.Context, string, []Source) (Generated, error)
}

type Source struct {
	FusionScore *float64 `json:"fusion_score,omitempty"`
	ChunkID     int64    `json:"chunk_id"`
	DocumentID  int64    `json:"document_id"`
	ChunkIndex  int      `json:"chunk_index"`
	SourceName  string   `json:"source_name"`
	Score       float64  `json:"score"`
	Excerpt     string   `json:"excerpt"`
	StartRune   int      `json:"start_rune"`
	EndRune     int      `json:"end_rune"`
}

type Usage struct {
	Known            bool   `json:"known"`
	ModelCalled      bool   `json:"model_called"`
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`
	CacheHitTokens   *int64 `json:"cache_hit_tokens"`
	CacheMissTokens  *int64 `json:"cache_miss_tokens"`
}

type Timings struct {
	EmbeddingMS  int64 `json:"embedding_ms"`
	SearchMS     int64 `json:"search_ms"`
	GenerationMS int64 `json:"generation_ms"`
	TotalMS      int64 `json:"total_ms"`
}

type Generated struct {
	Model                string
	Usage                Usage
	Answer               string
	CitationIDs          []int64
	InsufficientEvidence bool
}

// ValidateVectors validates the entire batch before any vectors are persisted or used.
func ValidateVectors(vectors [][]float32, count, dim int) error {
	if count < 0 || len(vectors) != count {
		return NewError("EMBEDDING_COUNT_MISMATCH")
	}
	if dim <= 0 {
		return NewError("EMBEDDING_DIMENSION_MISMATCH")
	}
	for _, vector := range vectors {
		if len(vector) != dim {
			return NewError("EMBEDDING_DIMENSION_MISMATCH")
		}
		var norm float64
		for _, value := range vector {
			v := float64(value)
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return NewError("EMBEDDING_INVALID_VECTOR")
			}
			norm += v * v
		}
		if norm == 0 {
			return NewError("EMBEDDING_INVALID_VECTOR")
		}
	}
	return nil
}

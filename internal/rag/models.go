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
	ChunkID    int64   `json:"chunk_id"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	SourceName string  `json:"source_name"`
	Score      float64 `json:"score"`
	Excerpt    string  `json:"excerpt"`
	StartRune  int     `json:"start_rune"`
	EndRune    int     `json:"end_rune"`
}

type Generated struct {
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

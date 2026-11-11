package llm

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

type embeddingClient struct{ client *Client }

func (*embeddingClient) Model() string  { return rag.EmbeddingModel }
func (*embeddingClient) Dimension() int { return rag.EmbeddingDimension }

func (e *embeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) < 1 || len(texts) > 8 {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	total := 0
	for _, text := range texts {
		if len(text) > 4000 || strings.TrimSpace(text) == "" || !utf8.ValidString(text) {
			return nil, rag.NewError("INVALID_REQUEST")
		}
		total += len(text)
	}
	if total > 32000 {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	request := struct {
		Model    string   `json:"model"`
		Input    []string `json:"input"`
		Truncate bool     `json:"truncate"`
	}{Model: rag.EmbeddingModel, Input: texts, Truncate: false}
	body, err := e.client.post(ctx, "/api/embed", request, embeddingTimeout, embeddingResponseLimit)
	if err != nil {
		return nil, err
	}
	var response struct {
		Model      string          `json:"model"`
		Embeddings [][]*float32    `json:"embeddings"`
		Error      json.RawMessage `json:"error"`
	}
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	if response.Model != rag.EmbeddingModel || len(response.Error) != 0 {
		return nil, rag.NewError("MODEL_INVALID_RESPONSE")
	}
	if len(response.Embeddings) != len(texts) {
		return nil, rag.NewError("EMBEDDING_COUNT_MISMATCH")
	}
	vectors := make([][]float32, len(response.Embeddings))
	for i, embedding := range response.Embeddings {
		if len(embedding) != rag.EmbeddingDimension {
			return nil, rag.NewError("EMBEDDING_DIMENSION_MISMATCH")
		}
		vectors[i] = make([]float32, len(embedding))
		for j, value := range embedding {
			if value == nil {
				return nil, rag.NewError("MODEL_INVALID_RESPONSE")
			}
			vectors[i][j] = *value
		}
	}
	if err := rag.ValidateVectors(vectors, len(texts), rag.EmbeddingDimension); err != nil {
		return nil, err
	}
	// Ollama normally returns unit vectors. Normalize defensively with float64
	// accumulation so valid extreme float32 inputs cannot overflow/underflow
	// pgvector's float32 norm accumulation when used for cosine distance.
	for _, vector := range vectors {
		var normSquared float64
		for _, value := range vector {
			normSquared += float64(value) * float64(value)
		}
		norm := math.Sqrt(normSquared)
		for i, value := range vector {
			vector[i] = float32(float64(value) / norm)
		}
	}
	return vectors, nil
}

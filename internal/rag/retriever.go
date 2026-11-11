package rag

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

func normalizeVectors(vectors [][]float32, count int) error {
	if err := ValidateVectors(vectors, count, EmbeddingDimension); err != nil {
		return err
	}
	for _, v := range vectors {
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		norm = math.Sqrt(norm)
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}
	return nil
}
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
func (s *Service) Search(ctx context.Context, actor, org int64, input QueryInput) (SearchResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.limits.SearchTimeout)
	defer cancel()
	result, _, err := s.retrieve(ctx, actor, org, input)
	return result, operationError(ctx, err, false)
}
func (s *Service) retrieve(ctx context.Context, actor, org int64, input QueryInput) (SearchResponse, []sourceSnapshot, error) {
	empty := SearchResponse{}
	if err := s.access.Authorize(ctx, actor, org); err != nil {
		return empty, nil, err
	}
	input, err := normalizeQuery(input)
	if err != nil {
		return empty, nil, err
	}
	if err = s.available(); err != nil {
		return empty, nil, err
	}
	start := time.Now()
	vectors, err := s.embedder.Embed(ctx, []string{input.Query})
	embeddingMS := time.Since(start).Milliseconds()
	if err != nil {
		return empty, nil, err
	}
	if err = normalizeVectors(vectors, 1); err != nil {
		return empty, nil, err
	}
	if err = s.access.Authorize(ctx, actor, org); err != nil {
		return empty, nil, err
	}
	if err = ctx.Err(); err != nil {
		return empty, nil, err
	}
	start = time.Now()
	rows, err := s.repo.Search(ctx, org, vectors[0], input.TopK, s.limits.MinimumScore)
	searchMS := time.Since(start).Milliseconds()
	if err != nil {
		return empty, nil, err
	}
	if err = s.revalidate(ctx, actor, org, rows); err != nil {
		return empty, nil, err
	}
	result := SearchResponse{Items: make([]Source, 0, len(rows)), EmbeddingModel: EmbeddingModel, EmbeddingMS: embeddingMS, SearchMS: searchMS}
	for _, row := range rows {
		result.Items = append(result.Items, row.Source)
	}
	return result, rows, nil
}

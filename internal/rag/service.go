package rag

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
)

type Access interface {
	Authorize(context.Context, int64, int64) error
	Get(context.Context, int64, int64, int64) (knowledge.Document, error)
	Text(context.Context, int64, int64, int64) (knowledge.DocumentText, error)
}
type Limits struct {
	Enabled                                 bool
	MinimumScore                            float64
	IndexTimeout, SearchTimeout, AskTimeout time.Duration
}
type QueryInput struct {
	RetrievalMode string `json:"retrieval_mode"`
	Query         string `json:"query"`
	TopK          int    `json:"top_k"`
}
type IndexInfo struct {
	DocumentID          int64      `json:"document_id"`
	Status              string     `json:"status"`
	IndexVersion        int64      `json:"index_version"`
	EmbeddingModel      string     `json:"embedding_model"`
	EmbeddingDimension  int        `json:"embedding_dimension"`
	ChunkerVersion      string     `json:"chunker_version"`
	SourceContentSHA256 string     `json:"source_content_sha256"`
	ChunkCount          int        `json:"chunk_count"`
	FailureCode         *string    `json:"failure_code"`
	StartedAt           *time.Time `json:"started_at"`
	IndexedAt           *time.Time `json:"indexed_at"`
	UpdatedAt           *time.Time `json:"updated_at"`
}
type SearchResponse struct {
	RetrievalMode  string   `json:"retrieval_mode"`
	Items          []Source `json:"items"`
	EmbeddingModel string   `json:"embedding_model"`
	EmbeddingMS    int64    `json:"embedding_ms"`
	SearchMS       int64    `json:"search_ms"`
}
type AskResponse struct {
	RetrievalMode        string   `json:"retrieval_mode"`
	Usage                Usage    `json:"usage"`
	Timings              Timings  `json:"timings"`
	Answer               string   `json:"answer"`
	Sources              []Source `json:"sources"`
	RequestID            string   `json:"request_id"`
	InsufficientEvidence bool     `json:"insufficient_evidence"`
	EmbeddingModel       string   `json:"embedding_model"`
	GenerationModel      string   `json:"generation_model"`
}
type indexClaim struct {
	Info    IndexInfo
	Content string
	Reused  bool
}
type sourceSnapshot struct {
	Source
	Hash    string
	Version int64
}
type repository interface {
	Claim(context.Context, int64, int64) (indexClaim, error)
	Finish(context.Context, int64, indexClaim, []knowledge.Chunk, [][]float32) (IndexInfo, error)
	Fail(context.Context, int64, int64, int64, string) error
	Status(context.Context, int64, int64) (IndexInfo, error)
	Search(context.Context, int64, []float32, int, float64) ([]sourceSnapshot, error)
	LexicalSearch(context.Context, int64, []float32, []string, int, float64) ([]sourceSnapshot, error)
	Validate(context.Context, int64, []sourceSnapshot) error
}
type Service struct {
	repo      repository
	access    Access
	embedder  Embedder
	generator Generator
	limits    Limits
}

func NewService(pool *pgxpool.Pool, access Access, embedder Embedder, generator Generator, limits Limits) *Service {
	if limits.IndexTimeout <= 0 {
		limits.IndexTimeout = 60 * time.Second
	}
	if limits.SearchTimeout <= 0 {
		limits.SearchTimeout = 25 * time.Second
	}
	if limits.AskTimeout <= 0 {
		limits.AskTimeout = 60 * time.Second
	}
	return &Service{repo: &indexRepository{pool: pool}, access: access, embedder: embedder, generator: generator, limits: limits}
}
func absentIndex(id int64) IndexInfo {
	return IndexInfo{DocumentID: id, Status: "ABSENT", EmbeddingModel: EmbeddingModel, EmbeddingDimension: EmbeddingDimension, ChunkerVersion: knowledge.ChunkerVersion}
}
func (s *Service) available() error {
	if !s.limits.Enabled || s.embedder == nil || s.generator == nil {
		return NewError("MODEL_UNAVAILABLE")
	}
	if s.embedder.Model() != EmbeddingModel || s.embedder.Dimension() != EmbeddingDimension || !SupportedChatModel(s.generator.Model()) {
		return NewError("MODEL_UNAVAILABLE")
	}
	return nil
}
func normalizeQuery(in QueryInput) (QueryInput, error) {
	if in.RetrievalMode == "" {
		in.RetrievalMode = "vector"
	}
	if in.RetrievalMode != "vector" && in.RetrievalMode != "hybrid" {
		return in, NewError("INVALID_REQUEST")
	}
	in.Query = strings.TrimSpace(in.Query)
	if !utf8.ValidString(in.Query) || strings.ContainsRune(in.Query, 0) || len(in.Query) > 4000 || utf8.RuneCountInString(in.Query) < 1 || utf8.RuneCountInString(in.Query) > 1000 {
		return in, NewError("INVALID_REQUEST")
	}
	if in.TopK == 0 {
		in.TopK = 5
	}
	if in.TopK < 1 || in.TopK > 10 {
		return in, NewError("INVALID_REQUEST")
	}
	return in, nil
}
func operationError(ctx context.Context, err error, index bool) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if index {
			return NewError("INDEX_TIMEOUT")
		}
		return NewError("MODEL_TIMEOUT")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func publicCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		if _, ok := errorMessages[e.Code]; ok {
			return e.Code
		}
	}
	switch {
	case errors.Is(err, knowledge.ErrInvalidRequest):
		return "INVALID_REQUEST"
	case errors.Is(err, knowledge.ErrUnauthenticated):
		return "UNAUTHENTICATED"
	case errors.Is(err, knowledge.ErrForbidden):
		return "FORBIDDEN"
	case errors.Is(err, knowledge.ErrNotFound):
		return "NOT_FOUND"
	case errors.Is(err, knowledge.ErrConflict):
		return "INDEX_STATE_CONFLICT"
	case errors.Is(err, context.DeadlineExceeded):
		return "MODEL_TIMEOUT"
	}
	return "INTERNAL_ERROR"
}
func (s *Service) IndexStatus(ctx context.Context, actor, org, id int64) (IndexInfo, error) {
	if _, err := s.access.Get(ctx, actor, org, id); err != nil {
		return IndexInfo{}, err
	}
	return s.repo.Status(ctx, org, id)
}
func (s *Service) revalidate(ctx context.Context, actor, org int64, rows []sourceSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.access.Authorize(ctx, actor, org); err != nil {
		return err
	}
	return s.repo.Validate(ctx, org, rows)
}

const insufficientAnswer = "知识库未检索到足够依据。"

func (s *Service) Ask(ctx context.Context, actor, org int64, input QueryInput) (AskResponse, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.limits.AskTimeout)
	defer cancel()
	result, rows, err := s.retrieve(ctx, actor, org, input)
	if err != nil {
		return AskResponse{}, operationError(ctx, err, false)
	}
	if observe.RequestID(ctx) == "" {
		ctx = observe.WithRequestID(ctx)
	}
	answer := AskResponse{Answer: insufficientAnswer, Sources: []Source{}, RequestID: observe.RequestID(ctx), InsufficientEvidence: true, EmbeddingModel: EmbeddingModel, GenerationModel: s.generator.Model()}
	answer.RetrievalMode = result.RetrievalMode
	answer.Timings = Timings{EmbeddingMS: result.EmbeddingMS, SearchMS: result.SearchMS}
	if len(rows) == 0 {
		zero := int64(0)
		answer.Usage = Usage{Known: true, PromptTokens: &zero, CompletionTokens: &zero, TotalTokens: &zero, CacheHitTokens: &zero, CacheMissTokens: &zero}
		answer.Timings.TotalMS = time.Since(started).Milliseconds()
		return answer, nil
	}
	// Revalidate the entire retrieval set, including any evidence the adapter may omit for its budget.
	if err = s.revalidate(ctx, actor, org, rows); err != nil {
		return AskResponse{}, operationError(ctx, err, false)
	}
	input, _ = normalizeQuery(input)
	generationStarted := time.Now()
	generated, err := s.generator.Generate(ctx, input.Query, result.Items)
	answer.Timings.GenerationMS = time.Since(generationStarted).Milliseconds()
	answer.Usage = generated.Usage
	if !answer.Usage.Known {
		answer.Usage = Usage{}
	}
	answer.Usage.ModelCalled = true
	if generated.Model != "" {
		answer.GenerationModel = generated.Model
	}
	if err = operationError(ctx, err, false); err != nil {
		return AskResponse{}, err
	}
	if err = s.revalidate(ctx, actor, org, rows); err != nil {
		return AskResponse{}, operationError(ctx, err, false)
	}
	answer.Timings.TotalMS = time.Since(started).Milliseconds()
	if generated.InsufficientEvidence {
		return answer, nil
	}
	text := strings.TrimSpace(generated.Answer)
	if text == "" || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || utf8.RuneCountInString(text) > 4000 || len(generated.CitationIDs) == 0 {
		return AskResponse{}, NewError("MODEL_INVALID_RESPONSE")
	}
	allowed := make(map[int64]Source, len(rows))
	for _, row := range rows {
		allowed[row.ChunkID] = row.Source
	}
	seen := make(map[int64]bool)
	for _, id := range generated.CitationIDs {
		source, ok := allowed[id]
		if !ok {
			return AskResponse{}, NewError("MODEL_INVALID_RESPONSE")
		}
		if !seen[id] {
			answer.Sources = append(answer.Sources, source)
			seen[id] = true
		}
	}
	answer.Answer = text
	answer.InsufficientEvidence = false
	answer.Timings.TotalMS = time.Since(started).Milliseconds()
	return answer, nil
}

package rag

import (
	"context"
	"errors"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
)

func (s *Service) Index(ctx context.Context, actor, org, id int64) (IndexInfo, error) {
	if err := s.access.Authorize(ctx, actor, org); err != nil {
		return IndexInfo{}, err
	}
	if id <= 0 {
		return IndexInfo{}, NewError("INVALID_REQUEST")
	}
	if err := s.available(); err != nil {
		return IndexInfo{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.limits.IndexTimeout)
	defer cancel()
	claim, err := s.repo.Claim(ctx, org, id)
	// An uncertain claim outcome must never mark an unowned run failed.
	if err != nil {
		return IndexInfo{}, operationError(ctx, err, true)
	}
	if claim.Reused {
		if err = s.access.Authorize(ctx, actor, org); err != nil {
			return IndexInfo{}, err
		}
		return claim.Info, nil
	}
	fail := func(cause error) (IndexInfo, error) {
		cause = operationError(ctx, cause, true)
		code := publicCode(cause)
		if errors.Is(cause, context.Canceled) {
			code = "PROCESS_CANCELED"
		} else {
			switch code {
			case "INDEX_CONTENT_INVALID", "INDEX_TOO_MANY_CHUNKS", "INDEX_SOURCE_CHANGED",
				"EMBEDDING_COUNT_MISMATCH", "EMBEDDING_DIMENSION_MISMATCH", "EMBEDDING_INVALID_VECTOR",
				"MODEL_INVALID_RESPONSE", "MODEL_UPSTREAM_ERROR", "MODEL_UNAVAILABLE", "MODEL_RATE_LIMITED",
				"MODEL_TIMEOUT", "RAG_BUSY", "INDEX_TIMEOUT":
			default:
				code = "INDEX_FAILED"
			}
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		_ = s.repo.Fail(cleanup, org, id, claim.Info.IndexVersion, code)
		return IndexInfo{}, cause
	}
	chunks, err := knowledge.SplitText(claim.Content)
	if errors.Is(err, knowledge.ErrChunkTooMany) {
		return fail(NewError("INDEX_TOO_MANY_CHUNKS"))
	}
	if err != nil {
		return fail(NewError("INDEX_CONTENT_INVALID"))
	}
	vectors := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += 8 {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		end := min(start+8, len(chunks))
		texts := make([]string, end-start)
		for i := start; i < end; i++ {
			texts[i-start] = chunks[i].Content
		}
		batch, e := s.embedder.Embed(ctx, texts)
		if e != nil {
			return fail(e)
		}
		if e = normalizeVectors(batch, len(texts)); e != nil {
			return fail(e)
		}
		vectors = append(vectors, batch...)
	}
	if err = s.access.Authorize(ctx, actor, org); err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	result, err := s.repo.Finish(ctx, org, claim, chunks, vectors)
	if err != nil {
		return fail(err)
	}
	return result, nil
}

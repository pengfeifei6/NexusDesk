package jobs

import (
	"context"
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskguard"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

// Error exposes only an allowlisted code and a fixed safe message.
type Error struct{ Code string }

var messages = map[string]string{
	"INVALID_REQUEST": "invalid request", "UNAUTHENTICATED": "authentication required", "FORBIDDEN": "permission denied", "NOT_FOUND": "resource not found", "JOB_STATE_CONFLICT": "job state conflict", "DOCUMENT_STATE_CONFLICT": "document state conflict", "ASYNC_DISABLED": "asynchronous processing disabled", "INTERNAL_ERROR": "internal operation failed", "DELIVERY_RETRY": "job delivery temporarily unavailable",
}

func (e *Error) Error() string {
	if e != nil {
		if m, ok := messages[e.Code]; ok {
			return m
		}
	}
	return messages["INTERNAL_ERROR"]
}
func newError(code string) error {
	if _, ok := messages[code]; !ok {
		code = "INTERNAL_ERROR"
	}
	return &Error{Code: code}
}
func Code(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		if _, ok := messages[e.Code]; ok {
			return e.Code
		}
	}
	return "INTERNAL_ERROR"
}
func classify(err error) (string, bool) {
	switch {
	case errors.Is(err, knowledge.ErrSourceChanged):
		return "SOURCE_CHANGED", false
	case errors.Is(err, taskguard.ErrRevoked), errors.Is(err, knowledge.ErrNotFound), errors.Is(err, knowledge.ErrForbidden), errors.Is(err, knowledge.ErrUnauthenticated):
		return "AUTHORIZATION_REVOKED", false
	case errors.Is(err, context.DeadlineExceeded):
		return "TASK_TIMEOUT", true
	case errors.Is(err, context.Canceled):
		return "PROCESS_CANCELED", true
	case errors.Is(err, knowledge.ErrInvalidRequest), errors.Is(err, knowledge.ErrUnsupported), errors.Is(err, knowledge.ErrTooLarge):
		return "INVALID_REQUEST", false
	case errors.Is(err, knowledge.ErrConflict):
		return "DOCUMENT_STATE_CONFLICT", false
	}
	var x *knowledge.ExtractionError
	if errors.As(err, &x) {
		switch x.Code {
		case "INVALID_TEXT", "EMPTY_TEXT", "TEXT_TOO_LARGE", "PDF_UNSUPPORTED":
			return x.Code, false
		}
	}
	var r *rag.Error
	if errors.As(err, &r) {
		switch r.Code {
		case "NOT_FOUND", "FORBIDDEN", "UNAUTHENTICATED":
			return "AUTHORIZATION_REVOKED", false
		case "MODEL_UNAVAILABLE", "MODEL_RATE_LIMITED", "MODEL_TIMEOUT", "INDEX_TIMEOUT", "RAG_BUSY":
			return r.Code, true
		// The adapter also uses this ambiguous code for deterministic 4xx
		// failures; only known transient codes above justify automatic retries.
		case "MODEL_UPSTREAM_ERROR", "INDEX_CONTENT_INVALID", "INDEX_TOO_MANY_CHUNKS", "INDEX_SOURCE_CHANGED", "EMBEDDING_COUNT_MISMATCH", "EMBEDDING_DIMENSION_MISMATCH", "EMBEDDING_INVALID_VECTOR", "MODEL_INVALID_RESPONSE", "INVALID_REQUEST", "INDEX_STATE_CONFLICT":
			return r.Code, false
		}
	}
	return "STORAGE_ERROR", true
}

package rag

// Error contains only an allowlisted public code. Upstream messages never enter it.
type Error struct{ Code string }

var errorMessages = map[string]string{
	"INVALID_REQUEST":              "invalid request",
	"UNAUTHENTICATED":              "authentication required",
	"FORBIDDEN":                    "permission denied",
	"NOT_FOUND":                    "resource not found",
	"INTERNAL_ERROR":               "internal operation failed",
	"INDEX_STATE_CONFLICT":         "document index state conflict",
	"INDEX_SOURCE_CHANGED":         "document source changed",
	"INDEX_CONTENT_INVALID":        "document content cannot be indexed",
	"INDEX_TOO_MANY_CHUNKS":        "document has too many chunks",
	"EMBEDDING_COUNT_MISMATCH":     "embedding count mismatch",
	"EMBEDDING_DIMENSION_MISMATCH": "embedding dimension mismatch",
	"EMBEDDING_INVALID_VECTOR":     "invalid embedding vector",
	"MODEL_INVALID_RESPONSE":       "invalid model response",
	"MODEL_UPSTREAM_ERROR":         "model operation failed",
	"MODEL_UNAVAILABLE":            "model unavailable",
	"MODEL_RATE_LIMITED":           "model rate limited",
	"MODEL_TIMEOUT":                "model request timed out",
	"INDEX_TIMEOUT":                "index operation timed out",
	"RAG_BUSY":                     "model capacity busy",
}

func (e *Error) Error() string {
	if e != nil {
		if message, ok := errorMessages[e.Code]; ok {
			return message
		}
	}
	return errorMessages["INTERNAL_ERROR"]
}

func NewError(code string) error {
	if _, ok := errorMessages[code]; !ok {
		code = "INTERNAL_ERROR"
	}
	return &Error{Code: code}
}

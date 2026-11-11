package llm_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

func requireCode(t *testing.T, err error, want string) {
	t.Helper()
	var e *rag.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("error = %v, want code %s", err, want)
	}
}

// A count, dimension, or non-finite check removed from ValidateVectors breaks this table.
func TestValidateVectors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		vectors    [][]float32
		count, dim int
		code       string
	}{
		{"valid", [][]float32{{1, 2}, {-1, 0}}, 2, 2, ""},
		{"count", [][]float32{{1, 2}}, 2, 2, "EMBEDDING_COUNT_MISMATCH"},
		{"dimension", [][]float32{{1}}, 1, 2, "EMBEDDING_DIMENSION_MISMATCH"},
		{"nil", [][]float32{nil}, 1, 2, "EMBEDDING_DIMENSION_MISMATCH"},
		{"zero", [][]float32{{0, 0}}, 1, 2, "EMBEDDING_INVALID_VECTOR"},
		{"nan", [][]float32{{float32(math.NaN()), 1}}, 1, 2, "EMBEDDING_INVALID_VECTOR"},
		{"infinity", [][]float32{{1, float32(math.Inf(1))}}, 1, 2, "EMBEDDING_INVALID_VECTOR"},
		{"negative infinity", [][]float32{{1, float32(math.Inf(-1))}}, 1, 2, "EMBEDDING_INVALID_VECTOR"},
		{"tiny nonzero", [][]float32{{math.SmallestNonzeroFloat32, 0}}, 1, 2, ""},
		{"large finite", [][]float32{{math.MaxFloat32, math.MaxFloat32}}, 1, 2, ""},
		{"invalid dimension", [][]float32{{}}, 1, 0, "EMBEDDING_DIMENSION_MISMATCH"},
		{"invalid count", nil, -1, 2, "EMBEDDING_COUNT_MISMATCH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rag.ValidateVectors(tc.vectors, tc.count, tc.dim)
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			requireCode(t, err, tc.code)
		})
	}
}

func TestModelErrorsDoNotExposeUnknownText(t *testing.T) {
	const private = "private upstream body http://credentials:secret@example.test"
	requireCode(t, rag.NewError(private), "INTERNAL_ERROR")
	if strings.Contains((&rag.Error{Code: private}).Error(), "private") {
		t.Fatal("unknown error code leaked into public text")
	}
	for _, code := range []string{"INVALID_REQUEST", "MODEL_INVALID_RESPONSE", "MODEL_UPSTREAM_ERROR", "MODEL_UNAVAILABLE", "MODEL_RATE_LIMITED", "MODEL_TIMEOUT", "RAG_BUSY", "INDEX_TIMEOUT", "NOT_FOUND", "INDEX_SOURCE_CHANGED", "INDEX_CONTENT_INVALID", "INDEX_STATE_CONFLICT", "INDEX_TOO_MANY_CHUNKS", "EMBEDDING_COUNT_MISMATCH", "EMBEDDING_DIMENSION_MISMATCH", "EMBEDDING_INVALID_VECTOR", "UNAUTHENTICATED", "FORBIDDEN", "INTERNAL_ERROR"} {
		err := rag.NewError(code)
		requireCode(t, err, code)
		if err.Error() == "" || strings.Contains(err.Error(), private) {
			t.Fatal("invalid public message")
		}
	}
}

func TestSourceKeepsNumericIDsAndSnakeCase(t *testing.T) {
	b, err := json.Marshal(rag.Source{ChunkID: 9007199254740993, DocumentID: 7, ChunkIndex: 0, SourceName: "sample.txt", Score: .9, Excerpt: "data", StartRune: 3, EndRune: 7})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"chunk_id":9007199254740993,"document_id":7,"chunk_index":0,"source_name":"sample.txt","score":0.9,"excerpt":"data","start_rune":3,"end_rune":7}`
	if string(b) != want {
		t.Fatalf("source JSON = %s", b)
	}
}

package rag

import (
	"context"
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"testing"
)

func TestAISearchValidationContract(t *testing.T) {
	s, r, a, _, _ := fixture()
	r.rows = evidence()
	result, validate, err := s.SearchWithValidation(context.Background(), 1, 2, QueryInput{Query: "q"})
	if err != nil || len(result.Items) != 2 || validate == nil {
		t.Fatalf("search: %v", err)
	}
	// The closure must keep the original private version/hash snapshot, not
	// reconstruct trust from the caller's mutable public source DTO.
	result.Items[0].ChunkID = 999
	r.validateFn = func(rows []sourceSnapshot) error {
		if rows[0].ChunkID != 11 || rows[0].Hash != "hash" || rows[0].Version != 1 {
			t.Fatal("snapshot was changed")
		}
		return NewError("INDEX_SOURCE_CHANGED")
	}
	codeIs(t, validate(context.Background()), "INDEX_SOURCE_CHANGED")
	a.err = knowledge.ErrNotFound
	if !errors.Is(validate(context.Background()), knowledge.ErrNotFound) {
		t.Fatal("revocation ignored")
	}
}

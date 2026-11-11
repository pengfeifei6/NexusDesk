package integration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

func week10RAGGate(t *testing.T) {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W10_RAG") != "1" {
		t.Skip("requires isolated W10 harness (NEXUSDESK_TEST_W10_RAG=1)")
	}
	week06Gate(t) // Fixture additionally enforces loopback and disposable DB name.
}

func TestWeek10HybridRetrieval(t *testing.T) {
	week10RAGGate(t)
	t.Run("literal_identifier_promoted_and_cosine_preserved", func(t *testing.T) {
		f := week06Fixture(t)
		s := week06Service(f, &week06Embedder{}, &week06Generator{})
		first := week06Ready(t, f, f.org, "unrelated first evidence")
		week06Index(t, f, s, first)
		match := week06Ready(t, f, f.org, "AUTH_KEY_42 知识库 范围")
		week06Index(t, f, s, match)
		baseline, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "AUTH_KEY_42 知识库", TopK: 1})
		if err != nil || len(baseline.Items) != 1 || baseline.Items[0].DocumentID != first.ID || baseline.RetrievalMode != "vector" || baseline.Items[0].FusionScore != nil {
			t.Fatalf("vector=%+v %v", baseline, err)
		}
		for i := 0; i < 3; i++ {
			hybrid, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "AUTH_KEY_42 知识库", TopK: 1, RetrievalMode: "hybrid"})
			if err != nil || len(hybrid.Items) != 1 || hybrid.Items[0].DocumentID != match.ID || hybrid.RetrievalMode != "hybrid" || hybrid.Items[0].FusionScore == nil || math.Abs(hybrid.Items[0].Score-1) > 1e-6 {
				t.Fatalf("hybrid=%+v %v", hybrid, err)
			}
		}
	})
	t.Run("both_candidate_lists_exclude_crossorg_stale_deleted_and_low_cosine", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		good := week06Ready(t, f, f.org, "AUTH_KEY_42 current")
		week06Index(t, f, s, good)
		for _, kind := range []string{"foreign", "text_hash", "chunk_hash", "index_hash", "chunk_version", "index_version", "deleted", "parent_failed", "index_failed", "indexing", "low_cosine"} {
			org := f.org
			if kind == "foreign" {
				org = f.otherOrg
			}
			d := week06Ready(t, f, org, "AUTH_KEY_42 excluded "+kind)
			if kind == "low_cosine" {
				e.fn = func(_ context.Context, texts []string) ([][]float32, error) {
					v := make([][]float32, len(texts))
					for i := range v {
						v[i] = week06Vector(0, 1)
					}
					return v, nil
				}
			}
			week06Index(t, f, s, d)
			e.fn = nil
			switch kind {
			case "text_hash":
				week06ChangeText(t, f, d, "AUTH_KEY_42 changed")
			case "chunk_hash":
				f.exec(t, "UPDATE knowledge_chunks SET content_sha256=$3 WHERE org_id=$1 AND document_id=$2", org, d.ID, strings.Repeat("a", 64))
			case "index_hash":
				f.exec(t, "UPDATE knowledge_document_indexes SET source_content_sha256=$3 WHERE org_id=$1 AND document_id=$2", org, d.ID, strings.Repeat("b", 64))
			case "chunk_version":
				f.exec(t, "UPDATE knowledge_chunks SET index_version=index_version+1 WHERE org_id=$1 AND document_id=$2", org, d.ID)
			case "index_version":
				f.exec(t, "UPDATE knowledge_document_indexes SET index_version=index_version+1 WHERE org_id=$1 AND document_id=$2", org, d.ID)
			case "deleted":
				f.exec(t, "UPDATE knowledge_documents SET deleted_at=clock_timestamp() WHERE org_id=$1 AND id=$2", org, d.ID)
			case "parent_failed":
				f.exec(t, "UPDATE knowledge_documents SET status='FAILED',extracted_chars=0,failure_code='PROCESS_FAILED' WHERE org_id=$1 AND id=$2", org, d.ID)
			case "index_failed":
				f.exec(t, "UPDATE knowledge_document_indexes SET status='FAILED',chunk_count=0,indexed_at=NULL,failure_code='INDEX_FAILED' WHERE org_id=$1 AND document_id=$2", org, d.ID)
			case "indexing":
				f.exec(t, "UPDATE knowledge_document_indexes SET status='INDEXING',chunk_count=0,indexed_at=NULL WHERE org_id=$1 AND document_id=$2", org, d.ID)
			}
		}
		got, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "AUTH_KEY_42", TopK: 10, RetrievalMode: "hybrid"})
		if err != nil || len(got.Items) != 1 || got.Items[0].DocumentID != good.ID {
			t.Fatalf("excluded source leaked %+v %v", got, err)
		}
		router := week06Router(f, s)
		path := fmt.Sprintf("/api/v1/organizations/%d/knowledge/search", f.org)
		knowledgeExpect(t, knowledgeCall(router, f.foreignToken, "POST", path, strings.NewReader(`{"query":"AUTH_KEY_42","retrieval_mode":"hybrid"}`), "application/json"), 404, "NOT_FOUND")
	})
	t.Run("uncited_fused_source_is_rechecked_after_generation", func(t *testing.T) {
		f := week06Fixture(t)
		var uncited knowledge.Document
		g := &week06Generator{fn: func(_ context.Context, _ string, sources []rag.Source) (rag.Generated, error) {
			if len(sources) != 2 {
				t.Fatalf("evidence=%v", sources)
			}
			f.exec(t, "UPDATE knowledge_documents SET deleted_at=clock_timestamp() WHERE org_id=$1 AND id=$2", f.org, uncited.ID)
			return rag.Generated{Answer: "must not escape", CitationIDs: []int64{sources[0].ChunkID}}, nil
		}}
		s := week06Service(f, &week06Embedder{}, g)
		first := week06Ready(t, f, f.org, "AUTH_KEY_42 first")
		week06Index(t, f, s, first)
		uncited = week06Ready(t, f, f.org, "AUTH_KEY_42 second")
		week06Index(t, f, s, uncited)
		got, err := s.Ask(f.ctx, f.peer, f.org, rag.QueryInput{Query: "AUTH_KEY_42", RetrievalMode: "hybrid"})
		week06Error(t, err, "INDEX_SOURCE_CHANGED")
		if got.Answer != "" || len(got.Sources) != 0 {
			t.Fatal("stale answer escaped")
		}
		f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.peer)
		_, err = s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "AUTH_KEY_42", RetrievalMode: "hybrid"})
		if !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatalf("revoked member=%v", err)
		}
	})
}

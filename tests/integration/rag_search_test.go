package integration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

func TestWeek06Search(t *testing.T) {
	week06Gate(t)
	t.Run("exact_cosine_order_ties_topk_threshold_and_plan", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		// Synthetic orthogonal coordinates exercise SQL math; they are not semantic embeddings.
		specs := []struct {
			text string
			x, y float32
		}{{"best-a", 1, 0}, {"best-b", 1, 0}, {"middle", 0.6, 0.8}, {"below-threshold", 0, 1}}
		docs := make([]knowledge.Document, 0, len(specs))
		for _, spec := range specs {
			e.fn = func(_ context.Context, texts []string) ([][]float32, error) {
				out := make([][]float32, len(texts))
				for i := range out {
					out[i] = week06Vector(spec.x, spec.y)
				}
				return out, nil
			}
			d := week06Ready(t, f, f.org, spec.text)
			week06Index(t, f, s, d)
			docs = append(docs, d)
		}
		e.fn = nil
		all, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "synthetic query", TopK: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(all.Items) != 3 {
			t.Fatalf("threshold returned %d rows", len(all.Items))
		}
		for i, src := range all.Items {
			if src.DocumentID != docs[i].ID || src.Excerpt != specs[i].text || src.StartRune != 0 || src.EndRune != len(specs[i].text) || src.SourceName != "synthetic-rag.md" {
				t.Fatal("SQL order or source metadata mismatch")
			}
		}
		if all.Items[0].ChunkID >= all.Items[1].ChunkID || math.Abs(all.Items[0].Score-1) > 1e-6 || math.Abs(all.Items[2].Score-0.6) > 1e-6 {
			t.Fatal("cosine score/tie ordering mismatch")
		}
		top, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "synthetic query", TopK: 2})
		if err != nil || len(top.Items) != 2 || top.Items[1].ChunkID != all.Items[1].ChunkID {
			t.Fatal("top_k not enforced")
		}
		if all.EmbeddingModel != rag.EmbeddingModel || all.EmbeddingMS < 0 || all.SearchMS < 0 {
			t.Fatal("timing/model metadata invalid")
		}
		// Same predicates and ordering as indexRepository.Search; no ANN index or quality/performance claim.
		rows, err := f.pool.Query(f.ctx, `EXPLAIN (ANALYZE, BUFFERS) SELECT c.id,c.document_id,c.chunk_index,d.original_filename,c.content_text,c.start_rune,c.end_rune,c.embedding <=> $2::vector AS distance,t.content_sha256,i.index_version FROM knowledge_chunks c
 JOIN knowledge_documents d ON d.org_id=c.org_id AND d.id=c.document_id
 JOIN knowledge_document_texts t ON t.org_id=c.org_id AND t.document_id=c.document_id
 JOIN knowledge_document_indexes i ON i.org_id=c.org_id AND i.document_id=c.document_id
 WHERE c.org_id=$1 AND d.deleted_at IS NULL AND d.status='READY' AND i.status='READY'
 AND i.source_content_sha256=t.content_sha256 AND c.content_sha256=t.content_sha256
 AND c.index_version=i.index_version AND c.embedding_model=i.embedding_model
 AND i.embedding_model='bge-m3:567m' AND i.embedding_dimension=1024
 AND c.chunker_version=i.chunker_version AND i.chunker_version='rune-500-80-v1'
 AND 1-(c.embedding <=> $2::vector)>=$3 ORDER BY distance ASC,c.id ASC LIMIT $4`, f.org, "[1,"+strings.Repeat("0,", 1022)+"0]", 0.4, 5)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if err = rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			t.Log(line)
		}
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
	})
	t.Run("only_current_hash_version_org_and_visible_ready_parent", func(t *testing.T) {
		f := week06Fixture(t)
		s := week06Service(f, &week06Embedder{}, &week06Generator{})
		current := week06Ready(t, f, f.org, "current valid evidence")
		week06Index(t, f, s, current)
		for _, kind := range []string{"foreign", "text_hash", "chunk_hash", "index_hash", "chunk_version", "index_version", "deleted", "parent_failed", "index_failed", "indexing"} {
			org := f.org
			if kind == "foreign" {
				org = f.otherOrg
			}
			d := week06Ready(t, f, org, "excluded evidence "+kind)
			week06Index(t, f, s, d)
			switch kind {
			case "text_hash":
				week06ChangeText(t, f, d, "source replaced")
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
		result, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "synthetic query", TopK: 10})
		if err != nil || len(result.Items) != 1 || result.Items[0].DocumentID != current.ID {
			t.Fatalf("stale/cross-org/invisible rows leaked: count=%d err=%v", len(result.Items), err)
		}
	})
	t.Run("HTTP_crossorg_disabled_and_identity", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		router := week06Router(f, s)
		d := week06Ready(t, f, f.org, "authorization fixture")
		base := fmt.Sprintf("/api/v1/organizations/%d/knowledge", f.org)
		for _, token := range []string{"", f.foreignToken, f.peerToken} {
			if token == f.peerToken {
				f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.peer)
			}
			want, code := 404, "NOT_FOUND"
			if token == "" {
				want, code = 401, "UNAUTHENTICATED"
			}
			for _, endpoint := range []string{"search", "ask"} {
				knowledgeExpect(t, knowledgeCall(router, token, "POST", base+"/"+endpoint, strings.NewReader(`{"query":"fixture"}`), "application/json"), want, code)
			}
			for _, method := range []string{"GET", "POST"} {
				knowledgeExpect(t, knowledgeCall(router, token, method, f.path(f.org, d.ID)+"/index", nil, ""), want, code)
			}
		}
		knowledgeExpect(t, knowledgeCall(router, f.foreignToken, "POST", f.path(f.otherOrg, d.ID)+"/index", nil, ""), 404, "NOT_FOUND")
		if e.calls.Load() != 0 {
			t.Fatal("unauthorized request called model")
		}
	})
	t.Run("membership_revoked_during_query_embedding", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		d := week06Ready(t, f, f.org, "revocation fixture")
		week06Index(t, f, s, d)
		e.fn = func(_ context.Context, texts []string) ([][]float32, error) {
			f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.peer)
			return week06Vectors(len(texts)), nil
		}
		result, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "fixture"})
		if !errors.Is(err, knowledge.ErrNotFound) || len(result.Items) != 0 {
			t.Fatal("revoked membership received evidence")
		}
	})
}

func TestWeek06Ask(t *testing.T) {
	week06Gate(t)
	t.Run("no_evidence_skips_generator", func(t *testing.T) {
		f := week06Fixture(t)
		g := &week06Generator{}
		s := week06Service(f, &week06Embedder{}, g)
		answer, err := s.Ask(f.ctx, f.peer, f.org, rag.QueryInput{Query: "unknown evidence"})
		if err != nil || !answer.InsufficientEvidence || answer.Answer != "知识库未检索到足够依据。" || answer.Sources == nil || len(answer.Sources) != 0 || g.calls.Load() != 0 || answer.RequestID == "" {
			t.Fatal("no-evidence refusal/generator contract failed")
		}
	})
	for _, kind := range []string{"unknown", "missing", "duplicate", "insufficient"} {
		t.Run("citations_"+kind, func(t *testing.T) {
			f := week06Fixture(t)
			g := &week06Generator{fn: func(_ context.Context, _ string, sources []rag.Source) (rag.Generated, error) {
				generated := rag.Generated{Answer: "Synthetic grounded answer", CitationIDs: []int64{sources[0].ChunkID}}
				switch kind {
				case "unknown":
					generated.CitationIDs = []int64{9223372036854775807}
				case "missing":
					generated.CitationIDs = nil
				case "duplicate":
					generated.CitationIDs = append(generated.CitationIDs, sources[0].ChunkID)
				case "insufficient":
					generated.InsufficientEvidence = true
				}
				return generated, nil
			}}
			s := week06Service(f, &week06Embedder{}, g)
			d := week06Ready(t, f, f.org, "Original grounded evidence")
			week06Index(t, f, s, d)
			answer, err := s.Ask(f.ctx, f.peer, f.org, rag.QueryInput{Query: "synthetic query"})
			switch kind {
			case "unknown", "missing":
				week06Error(t, err, "MODEL_INVALID_RESPONSE")
				if answer.Answer != "" || len(answer.Sources) != 0 {
					t.Fatal("invalid model output escaped")
				}
			case "duplicate":
				if err != nil || len(answer.Sources) != 1 || answer.Sources[0].DocumentID != d.ID || answer.Sources[0].Excerpt != "Original grounded evidence" || answer.InsufficientEvidence {
					t.Fatal("citations not deduplicated/constructed from stored evidence")
				}
			case "insufficient":
				if err != nil || !answer.InsufficientEvidence || len(answer.Sources) != 0 || answer.Answer != "知识库未检索到足够依据。" {
					t.Fatal("model insufficiency was not normalized")
				}
			}
		})
	}
	for _, mutation := range []string{"delete_cited", "delete_uncited", "disable", "hash_cited", "hash_uncited", "version_uncited"} {
		t.Run("recheck_after_generation_"+mutation, func(t *testing.T) {
			f := week06Fixture(t)
			entered, release := make(chan []rag.Source, 1), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			g := &week06Generator{fn: func(ctx context.Context, _ string, sources []rag.Source) (rag.Generated, error) {
				entered <- sources
				select {
				case <-release:
					return rag.Generated{Answer: "This answer must never escape", CitationIDs: []int64{sources[0].ChunkID}}, nil
				case <-ctx.Done():
					return rag.Generated{}, ctx.Err()
				}
			}}
			s := week06Service(f, &week06Embedder{}, g)
			first := week06Ready(t, f, f.org, "first evidence")
			second := week06Ready(t, f, f.org, "second evidence")
			week06Index(t, f, s, first)
			week06Index(t, f, s, second)
			type outcome struct {
				answer rag.AskResponse
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				a, err := s.Ask(f.ctx, f.peer, f.org, rag.QueryInput{Query: "synthetic query"})
				done <- outcome{a, err}
			}()
			sources := week06Await(t, entered)
			if len(sources) != 2 || sources[0].DocumentID != first.ID || sources[1].DocumentID != second.ID {
				t.Fatal("generator did not receive both real evidence rows")
			}
			target := first
			if strings.Contains(mutation, "uncited") {
				target = second
			}
			switch {
			case mutation == "disable":
				f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.peer)
			case strings.HasPrefix(mutation, "delete"):
				f.exec(t, "UPDATE knowledge_documents SET deleted_at=clock_timestamp() WHERE org_id=$1 AND id=$2", f.org, target.ID)
			case strings.HasPrefix(mutation, "hash"):
				week06ChangeText(t, f, target, "changed while generating")
			case strings.HasPrefix(mutation, "version"):
				f.exec(t, "UPDATE knowledge_document_indexes SET index_version=index_version+1 WHERE org_id=$1 AND document_id=$2", f.org, target.ID)
			}
			once.Do(func() { close(release) })
			got := week06Await(t, done)
			if mutation == "disable" {
				if !errors.Is(got.err, knowledge.ErrNotFound) {
					t.Fatalf("expected revoked 404, got %v", got.err)
				}
			} else {
				week06Error(t, got.err, "INDEX_SOURCE_CHANGED")
			}
			if got.answer.Answer != "" || len(got.answer.Sources) != 0 {
				t.Fatal("answer from stale/revoked evidence escaped")
			}
		})
	}
}

func TestWeek06DatabaseDeadlines(t *testing.T) {
	week06Gate(t)
	t.Run("status_table_lock_has_three_second_budget", func(t *testing.T) {
		f := week06Fixture(t)
		s := week06Service(f, &week06Embedder{}, &week06Generator{})
		d := week06Ready(t, f, f.org, "status timeout fixture")
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(f.ctx, "LOCK TABLE knowledge_document_indexes IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		start := time.Now()
		_, err = s.IndexStatus(ctx, f.peer, f.org, d.ID)
		elapsed := time.Since(start)
		week06Error(t, err, "INDEX_TIMEOUT")
		if elapsed > 4*time.Second || elapsed < 2*time.Second {
			t.Fatalf("status DB budget: elapsed=%s err=%v", elapsed, err)
		}
	})
	t.Run("claim_row_lock_has_five_second_budget", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		d := week06Ready(t, f, f.org, "claim timeout fixture")
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(f.ctx, "SELECT id FROM knowledge_documents WHERE org_id=$1 AND id=$2 FOR UPDATE", f.org, d.ID); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
		defer cancel()
		start := time.Now()
		_, err = s.Index(ctx, f.peer, f.org, d.ID)
		elapsed := time.Since(start)
		week06Error(t, err, "INDEX_TIMEOUT")
		if elapsed > 7*time.Second || elapsed < 4*time.Second || e.calls.Load() != 0 {
			t.Fatalf("claim DB budget: elapsed=%s model_calls=%d", elapsed, e.calls.Load())
		}
	})
}

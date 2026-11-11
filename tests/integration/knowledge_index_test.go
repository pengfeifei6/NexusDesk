package integration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

// The fakes below control only model boundaries. All authorization, claims,
// publication, failure recording and vector retrieval use real PostgreSQL.
// These tests measure contracts and races, never semantic retrieval quality.
type week06Embedder struct {
	calls atomic.Int32
	fn    func(context.Context, []string) ([][]float32, error)
}

func (*week06Embedder) Model() string  { return rag.EmbeddingModel }
func (*week06Embedder) Dimension() int { return rag.EmbeddingDimension }
func (e *week06Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls.Add(1)
	if e.fn != nil {
		return e.fn(ctx, texts)
	}
	return week06Vectors(len(texts)), nil
}

type week06Generator struct {
	calls atomic.Int32
	fn    func(context.Context, string, []rag.Source) (rag.Generated, error)
}

func (*week06Generator) Model() string { return rag.ChatModel }
func (g *week06Generator) Generate(ctx context.Context, query string, sources []rag.Source) (rag.Generated, error) {
	g.calls.Add(1)
	if g.fn != nil {
		return g.fn(ctx, query, sources)
	}
	return rag.Generated{Answer: "Synthetic grounded answer", CitationIDs: []int64{sources[0].ChunkID}}, nil
}
func week06Vector(x, y float32) []float32 {
	v := make([]float32, rag.EmbeddingDimension)
	v[0], v[1] = x, y
	return v
}
func week06Vectors(n int) [][]float32 {
	out := make([][]float32, n)
	for i := range out {
		out[i] = week06Vector(1, 0)
	}
	return out
}
func week06Gate(t *testing.T) {
	t.Helper()
	for _, gate := range []string{"NEXUSDESK_TEST_W06_RAG", "NEXUSDESK_TEST_W05_KNOWLEDGE", "NEXUSDESK_TEST_W03_TICKETS"} {
		if os.Getenv(gate) != "1" {
			t.Skip("requires scripts/week06-smoke.py --backend-only (" + gate + ")")
		}
	}
}
func week06Fixture(t *testing.T) *knowledgeFixture {
	t.Helper()
	week06Gate(t)
	return newKnowledgeFixture(t) // W05 gate plus ticketDB loopback/disposable-name guard.
}
func week06Service(f *knowledgeFixture, e *week06Embedder, g *week06Generator) *rag.Service {
	return rag.NewService(f.pool, f.service, e, g, rag.Limits{Enabled: true, MinimumScore: 0.4})
}
func week06Ready(t *testing.T, f *knowledgeFixture, org int64, text string) knowledge.Document {
	t.Helper()
	token := f.uploadToken
	if org == f.otherOrg {
		token = f.foreignToken
	}
	d := f.upload(t, token, org, "synthetic-rag.md", []byte(text))
	knowledgeExpect(t, f.call(token, "POST", f.path(org, d.ID)+"/process"), 200, "")
	return d
}
func week06Index(t *testing.T, f *knowledgeFixture, s *rag.Service, d knowledge.Document) rag.IndexInfo {
	t.Helper()
	actor := f.uploader
	if d.OrgID == f.otherOrg {
		actor = f.foreign
	}
	info, err := s.Index(f.ctx, actor, d.OrgID, d.ID)
	if err != nil || info.Status != "READY" || info.ChunkCount < 1 {
		t.Fatalf("index failed: status=%s err=%v", info.Status, err)
	}
	return info
}
func week06Error(t *testing.T, err error, code string) {
	t.Helper()
	var e *rag.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func week06State(t *testing.T, f *knowledgeFixture, id int64, status, code string, version int64) {
	t.Helper()
	var got string
	var failure *string
	var v int64
	if err := f.pool.QueryRow(f.ctx, "SELECT status,failure_code,index_version FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2", f.org, id).Scan(&got, &failure, &v); err != nil {
		t.Fatal(err)
	}
	if got != status || v != version || (code != "" && (failure == nil || *failure != code)) || (code == "" && failure != nil) {
		t.Fatalf("state=%s version=%d failure=%v; want %s/%d/%s", got, v, failure, status, version, code)
	}
}
func week06ChangeText(t *testing.T, f *knowledgeFixture, d knowledge.Document, text string) {
	t.Helper()
	f.exec(t, "UPDATE knowledge_document_texts SET content_text=$3,content_sha256=$4 WHERE org_id=$1 AND document_id=$2", d.OrgID, d.ID, text, knowledgeHash([]byte(text)))
}
func week06Await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(8 * time.Second):
		t.Fatal("fixture barrier timed out")
	}
	var zero T
	return zero
}
func week06Router(f *knowledgeFixture, s *rag.Service) *gin.Engine {
	return httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(f.tokens, f.users))
		rag.NewHandler(s).RegisterRoutes(g)
	})
}

func TestWeek06Schema(t *testing.T) {
	week06Gate(t)
	f := week06Fixture(t)
	s := week06Service(f, &week06Embedder{}, &week06Generator{})
	d := week06Ready(t, f, f.org, "Synthetic schema evidence")
	week06Index(t, f, s, d)
	var typ string
	if err := f.pool.QueryRow(f.ctx, `SELECT format_type(a.atttypid,a.atttypmod) FROM pg_attribute a WHERE a.attrelid='knowledge_chunks'::regclass AND a.attname='embedding'`).Scan(&typ); err != nil || typ != "vector(1024)" {
		t.Fatalf("embedding column=%s err=%v", typ, err)
	}
	bad := func(name, code, sql string, args ...any) {
		t.Run(name, func(t *testing.T) {
			_, err := f.pool.Exec(f.ctx, sql, args...)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != code {
				t.Fatalf("wanted SQLSTATE %s, got %v", code, err)
			}
		})
	}
	for _, tc := range []struct{ name, expr string }{
		{"chunk_index", "chunk_index=200"}, {"negative_offset", "start_rune=-1"}, {"offset_length", "end_rune=end_rune+1"}, {"hash", "content_sha256='invalid'"}, {"model", "embedding_model='another-model'"}, {"chunker", "chunker_version='other'"}, {"version", "index_version=0"}, {"empty_text", "content_text=''"}, {"zero_vector", "embedding=array_fill(0::real,ARRAY[1024])::vector"},
	} {
		bad("chunk_check_"+tc.name, "23514", "UPDATE knowledge_chunks SET "+tc.expr+" WHERE org_id=$1 AND document_id=$2", f.org, d.ID)
	}
	bad("vector_dimension", "22000", "UPDATE knowledge_chunks SET embedding='[1,0]'::vector WHERE org_id=$1 AND document_id=$2", f.org, d.ID)
	bad("chunk_scoped_fk", "23503", "UPDATE knowledge_chunks SET org_id=$1 WHERE org_id=$2 AND document_id=$3", f.otherOrg, f.org, d.ID)
	bad("index_scoped_fk", "23503", "UPDATE knowledge_document_indexes SET org_id=$1 WHERE org_id=$2 AND document_id=$3", f.otherOrg, f.org, d.ID)
	for _, tc := range []struct{ name, expr string }{
		{"dimension", "embedding_dimension=3"}, {"model", "embedding_model='other'"}, {"chunker", "chunker_version='other'"}, {"hash", "source_content_sha256='invalid'"}, {"version", "index_version=0"}, {"status", "status='ABSENT'"}, {"ready_count", "chunk_count=0"}, {"ready_timestamp", "indexed_at=NULL"}, {"failed_code", "status='FAILED',chunk_count=0,indexed_at=NULL"}, {"unknown_failure", "status='FAILED',chunk_count=0,indexed_at=NULL,failure_code='private error'"}, {"ready_failure", "failure_code='INDEX_FAILED'"},
	} {
		bad("index_check_"+tc.name, "23514", "UPDATE knowledge_document_indexes SET "+tc.expr+" WHERE org_id=$1 AND document_id=$2", f.org, d.ID)
	}
	bad("unique_chunk_position", "23505", `INSERT INTO knowledge_chunks(org_id,document_id,chunk_index,content_text,start_rune,end_rune,content_sha256,embedding,embedding_model,chunker_version,index_version) SELECT org_id,document_id,chunk_index,content_text,start_rune,end_rune,content_sha256,embedding,embedding_model,chunker_version,index_version FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2`, f.org, d.ID)
}

func TestWeek06Index(t *testing.T) {
	week06Gate(t)
	t.Run("HTTP_uploaded_failed_and_deleted_sources_reject_before_claim", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		router := week06Router(f, s)
		uploaded := f.upload(t, f.uploadToken, f.org, "uploaded.md", []byte("Unprocessed synthetic evidence"))
		knowledgeExpect(t, knowledgeCall(router, f.peerToken, "POST", f.path(f.org, uploaded.ID)+"/index", nil, ""), 409, "INDEX_STATE_CONFLICT")
		failed := f.upload(t, f.uploadToken, f.org, "blank.md", []byte(" \t\r\n "))
		knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, failed.ID)+"/process"), 422, "EMPTY_TEXT")
		f.assertNoText(t, failed, "EMPTY_TEXT")
		knowledgeExpect(t, knowledgeCall(router, f.peerToken, "POST", f.path(f.org, failed.ID)+"/index", nil, ""), 409, "INDEX_STATE_CONFLICT")
		deleted := week06Ready(t, f, f.org, "Deleted synthetic evidence")
		knowledgeExpect(t, f.call(f.uploadToken, "DELETE", f.path(f.org, deleted.ID)), 204, "")
		knowledgeExpect(t, knowledgeCall(router, f.peerToken, "POST", f.path(f.org, deleted.ID)+"/index", nil, ""), 404, "NOT_FOUND")
		if e.calls.Load() != 0 || f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE org_id=$1", f.org) != 0 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE org_id=$1", f.org) != 0 {
			t.Fatal("non-READY/deleted source claimed an index, stored chunks or called embedder")
		}
	})
	t.Run("HTTP_chunk_limit_records_failure_without_embedding", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		// One rune beyond the last full 200th window requires a 201st chunk.
		text := strings.Repeat("界", 500+420*199+1)
		d := week06Ready(t, f, f.org, text)
		rec := knowledgeCall(week06Router(f, s), f.peerToken, "POST", f.path(f.org, d.ID)+"/index", nil, "")
		knowledgeExpect(t, rec, 422, "INDEX_TOO_MANY_CHUNKS")
		week06State(t, f, d.ID, "FAILED", "INDEX_TOO_MANY_CHUNKS", 1)
		if e.calls.Load() != 0 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 0 || f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 AND chunk_count=0 AND indexed_at IS NULL", f.org, d.ID) != 1 {
			t.Fatal("oversized chunk set embedded or published partial index")
		}
	})
	t.Run("ready_reuse_offsets_and_retry", func(t *testing.T) {
		f := week06Fixture(t)
		e := &week06Embedder{}
		s := week06Service(f, e, &week06Generator{})
		text := strings.Repeat("组织A", 350)
		d := week06Ready(t, f, f.org, text)
		absent, err := s.IndexStatus(f.ctx, f.peer, f.org, d.ID)
		if err != nil || absent.Status != "ABSENT" || absent.IndexVersion != 0 {
			t.Fatal("missing ABSENT status")
		}
		first := week06Index(t, f, s, d)
		if first.IndexVersion != 1 || first.SourceContentSHA256 != knowledgeHash([]byte(text)) {
			t.Fatal("wrong snapshot/version")
		}
		calls := e.calls.Load()
		again := week06Index(t, f, s, d)
		if again.IndexVersion != 1 || e.calls.Load() != calls {
			t.Fatal("same hash was reembedded")
		}
		rows, err := f.pool.Query(f.ctx, "SELECT chunk_index,content_text,start_rune,end_rune,content_sha256,index_version,vector_dims(embedding) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2 ORDER BY chunk_index", f.org, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var i, start, end, dim int
			var content, hash string
			var v int64
			if err = rows.Scan(&i, &content, &start, &end, &hash, &v, &dim); err != nil {
				t.Fatal(err)
			}
			if i != n || content != string([]rune(text)[start:end]) || hash != first.SourceContentSHA256 || v != 1 || dim != 1024 {
				t.Fatal("stored chunk snapshot differs")
			}
			n++
		}
		if rows.Err() != nil || n != first.ChunkCount || n < 2 {
			t.Fatal("chunk rows/count mismatch")
		}
		rows.Close()
		week06ChangeText(t, f, d, text+" changed")
		e.fn = func(context.Context, []string) ([][]float32, error) { return nil, rag.NewError("MODEL_UNAVAILABLE") }
		_, err = s.Index(f.ctx, f.peer, f.org, d.ID)
		week06Error(t, err, "MODEL_UNAVAILABLE")
		week06State(t, f, d.ID, "FAILED", "MODEL_UNAVAILABLE", 2)
		e.fn = nil
		retry := week06Index(t, f, s, d)
		if retry.IndexVersion != 3 {
			t.Fatal("FAILED retry did not increment version")
		}
	})
	for _, tc := range []struct {
		name, code string
		vectors    func(int) [][]float32
	}{
		{"count", "EMBEDDING_COUNT_MISMATCH", func(int) [][]float32 { return nil }},
		{"dimension", "EMBEDDING_DIMENSION_MISMATCH", func(n int) [][]float32 { v := week06Vectors(n); v[0] = []float32{1}; return v }},
		{"zero", "EMBEDDING_INVALID_VECTOR", func(n int) [][]float32 { v := week06Vectors(n); v[0] = make([]float32, 1024); return v }},
		{"nan", "EMBEDDING_INVALID_VECTOR", func(n int) [][]float32 { v := week06Vectors(n); v[0][1] = float32(math.NaN()); return v }},
		{"infinity", "EMBEDDING_INVALID_VECTOR", func(n int) [][]float32 { v := week06Vectors(n); v[0][1] = float32(math.Inf(1)); return v }},
	} {
		t.Run("invalid_"+tc.name, func(t *testing.T) {
			f := week06Fixture(t)
			d := week06Ready(t, f, f.org, "Synthetic invalid-vector evidence")
			e := &week06Embedder{fn: func(_ context.Context, texts []string) ([][]float32, error) { return tc.vectors(len(texts)), nil }}
			s := week06Service(f, e, &week06Generator{})
			_, err := s.Index(f.ctx, f.uploader, f.org, d.ID)
			week06Error(t, err, tc.code)
			week06State(t, f, d.ID, "FAILED", tc.code, 1)
			if f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 0 {
				t.Fatal("invalid vectors were persisted")
			}
		})
	}
	t.Run("parallel_claim_and_model_has_no_row_locks", func(t *testing.T) {
		f := week06Fixture(t)
		d := week06Ready(t, f, f.org, "Synthetic concurrent evidence")
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		e := &week06Embedder{fn: func(ctx context.Context, texts []string) ([][]float32, error) {
			close(entered)
			select {
			case <-release:
				return week06Vectors(len(texts)), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}
		s := week06Service(f, e, &week06Generator{})
		done := make(chan error, 1)
		go func() { _, err := s.Index(f.ctx, f.uploader, f.org, d.ID); done <- err }()
		week06Await(t, entered)
		// NOWAIT proves all source/index row locks from claim were released before model I/O.
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, sql := range []string{"SELECT id FROM knowledge_documents WHERE org_id=$1 AND id=$2 FOR UPDATE NOWAIT", "SELECT document_id FROM knowledge_document_texts WHERE org_id=$1 AND document_id=$2 FOR UPDATE NOWAIT", "SELECT document_id FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 FOR UPDATE NOWAIT"} {
			if _, err = tx.Exec(f.ctx, sql, f.org, d.ID); err != nil {
				_ = tx.Rollback(context.Background())
				t.Fatalf("model call holds source lock: %v", err)
			}
		}
		if err = tx.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		conflictCtx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
		defer cancel()
		_, err = s.Index(conflictCtx, f.peer, f.org, d.ID)
		week06Error(t, err, "INDEX_STATE_CONFLICT")
		if e.calls.Load() != 1 {
			t.Fatal("losing claim called embedder")
		}
		once.Do(func() { close(release) })
		if err = week06Await(t, done); err != nil {
			t.Fatal(err)
		}
		week06State(t, f, d.ID, "READY", "", 1)
	})
	for _, mutation := range []string{"hash", "softdelete"} {
		t.Run("mutation_during_embedding_"+mutation, func(t *testing.T) {
			f := week06Fixture(t)
			d := week06Ready(t, f, f.org, "Synthetic race evidence")
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			e := &week06Embedder{fn: func(ctx context.Context, texts []string) ([][]float32, error) {
				close(entered)
				select {
				case <-release:
					return week06Vectors(len(texts)), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			s := week06Service(f, e, &week06Generator{})
			done := make(chan error, 1)
			go func() { _, err := s.Index(f.ctx, f.uploader, f.org, d.ID); done <- err }()
			week06Await(t, entered)
			if mutation == "hash" {
				week06ChangeText(t, f, d, "Changed source evidence")
			} else {
				f.exec(t, "UPDATE knowledge_documents SET deleted_at=clock_timestamp() WHERE org_id=$1 AND id=$2", f.org, d.ID)
			}
			once.Do(func() { close(release) })
			week06Error(t, week06Await(t, done), "INDEX_SOURCE_CHANGED")
			if f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 AND status='READY'", f.org, d.ID) != 0 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 0 {
				t.Fatal("stale/deleted source published")
			}
			if mutation == "hash" {
				week06State(t, f, d.ID, "FAILED", "INDEX_SOURCE_CHANGED", 1)
			} else {
				week06State(t, f, d.ID, "INDEXING", "", 1)
			}
		})
	}
	t.Run("cancel_records_failure_with_independent_context", func(t *testing.T) {
		f := week06Fixture(t)
		d := week06Ready(t, f, f.org, "Synthetic canceled evidence")
		entered := make(chan struct{})
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		e := &week06Embedder{fn: func(ctx context.Context, _ []string) ([][]float32, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		s := week06Service(f, e, &week06Generator{})
		done := make(chan error, 1)
		go func() { _, err := s.Index(ctx, f.uploader, f.org, d.ID); done <- err }()
		week06Await(t, entered)
		cancel()
		if !errors.Is(week06Await(t, done), context.Canceled) {
			t.Fatal("cancellation not propagated")
		}
		week06State(t, f, d.ID, "FAILED", "PROCESS_CANCELED", 1)
	})
	t.Run("midinsert_failure_rolls_back_replacement", func(t *testing.T) {
		f := week06Fixture(t)
		d := week06Ready(t, f, f.org, "Original evidence")
		s := week06Service(f, &week06Embedder{}, &week06Generator{})
		week06Index(t, f, s, d)
		var oldID int64
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2", f.org, d.ID).Scan(&oldID); err != nil {
			t.Fatal(err)
		}
		week06ChangeText(t, f, d, strings.Repeat("replacement evidence ", 60))
		// Fail only this test's owned document at its second INSERT, after DELETE and first INSERT.
		f.exec(t, fmt.Sprintf(`CREATE FUNCTION nexusdesk_w06_fail_%d() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.document_id=%d AND NEW.chunk_index=1 THEN RAISE EXCEPTION 'private SQL fixture'; END IF; RETURN NEW; END $$`, d.ID, d.ID))
		f.exec(t, fmt.Sprintf("CREATE TRIGGER nexusdesk_w06_failure_%d BEFORE INSERT ON knowledge_chunks FOR EACH ROW EXECUTE FUNCTION nexusdesk_w06_fail_%d()", d.ID, d.ID))
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := f.pool.Exec(ctx, fmt.Sprintf("DROP TRIGGER nexusdesk_w06_failure_%d ON knowledge_chunks; DROP FUNCTION nexusdesk_w06_fail_%d()", d.ID, d.ID)); err != nil {
				t.Error("owned trigger cleanup failed")
			}
		})
		_, err := s.Index(f.ctx, f.uploader, f.org, d.ID)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Message != "private SQL fixture" {
			t.Fatalf("second-insert fault was not reached: %v", err)
		}
		week06State(t, f, d.ID, "FAILED", "INDEX_FAILED", 2)
		if f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2 AND id=$3 AND index_version=1 AND content_text='Original evidence'", f.org, d.ID, oldID) != 1 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 1 {
			t.Fatal("replacement did not roll back atomically")
		}
		result, err := s.Search(f.ctx, f.peer, f.org, rag.QueryInput{Query: "evidence"})
		if err != nil || len(result.Items) != 0 {
			t.Fatal("rolled-back chunks became searchable through FAILED index")
		}
	})
}

package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskqueue"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

// Real PostgreSQL owns all state below. Only external model scheduling/results
// are controlled. SQL time/attempt injection is not evidence of an OS crash;
// scripts/week07-process.py supplies independent process kill/restart evidence.
func week07Fixture(t *testing.T) *knowledgeFixture {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W07_JOBS") != "1" {
		t.Skip("requires owned scripts/week07-smoke.py --backend-only")
	}
	f := week06Fixture(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := f.pool.Exec(ctx, `UPDATE knowledge_jobs SET status='CANCELED',lease_token=NULL,lease_until=NULL,completed_at=clock_timestamp() WHERE org_id=$1 AND status IN ('PENDING','RUNNING','RETRYING')`, f.org)
		if err != nil {
			t.Error("owned job cleanup failed")
		}
	})
	return f
}
func week07Service(f *knowledgeFixture, e *week06Embedder) *jobs.Service {
	return jobs.NewService(f.pool, f.service, week06Service(f, e, &week06Generator{}), jobs.Limits{Enabled: true, HeartbeatInterval: 100 * time.Millisecond})
}
func week07Submit(t *testing.T, f *knowledgeFixture, s *jobs.Service, content string) (knowledge.Document, jobs.Job) {
	t.Helper()
	d := f.upload(t, f.uploadToken, f.org, "w07-synthetic.md", []byte(content))
	j, e := s.Submit(f.ctx, f.uploader, f.org, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	return d, j
}
func week07Payload(j jobs.Job) jobs.Payload {
	return jobs.Payload{JobID: j.ID, OrgID: j.OrgID, RunVersion: j.RunVersion}
}
func week07Get(t *testing.T, f *knowledgeFixture, s *jobs.Service, j jobs.Job, status string) jobs.Job {
	t.Helper()
	got, e := s.Get(f.ctx, f.admin, f.org, j.ID)
	if e != nil || got.Status != status {
		t.Fatalf("job status=%s want=%s error=%v", got.Status, status, e)
	}
	return got
}
func week07Execute(t *testing.T, f *knowledgeFixture, s *jobs.Service, j jobs.Job) {
	t.Helper()
	if e := s.Execute(f.ctx, week07Payload(j)); e != nil {
		t.Fatal(e)
	}
}

type week07Queue func(context.Context, jobs.Payload, int64) error

func (q week07Queue) Enqueue(c context.Context, p jobs.Payload, n int64) error { return q(c, p, n) }

var week07Discard = week07Queue(func(context.Context, jobs.Payload, int64) error { return nil })

func week07Router(f *knowledgeFixture, s *jobs.Service) *gin.Engine {
	return httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(f.tokens, f.users))
		jobs.NewHandler(s).RegisterRoutes(g)
	})
}

func TestWeek07SchemaAndHTTP(t *testing.T) {
	f := week07Fixture(t)
	s := week07Service(f, &week06Embedder{})
	d, j := week07Submit(t, f, s, "HTTP and schema fixture")
	for _, tc := range []struct{ name, expr, code string }{
		{"attempts", "attempts=4,total_attempts=4", "23514"}, {"total", "attempts=1,total_attempts=0", "23514"}, {"max", "max_attempts=4", "23514"}, {"version", "run_version=0", "23514"}, {"status", "status='unknown'", "23514"}, {"phase", "phase='unknown'", "23514"}, {"token", "lease_token='unsafe'", "23514"}, {"running_lease", "status='RUNNING'", "23514"}, {"terminal_time", "status='FAILED'", "23514"}, {"hash", "source_sha256='invalid'", "23514"}, {"model", "embedding_model='other'", "23514"}, {"dimension", "embedding_dimension=1", "23514"}, {"failure", "failure_code='private upstream body'", "23514"}, {"dispatch_error", "last_dispatch_error='private redis URL'", "23514"}, {"dispatch_seq", "dispatch_seq=-1", "23514"}, {"document_scope", fmt.Sprintf("org_id=%d", f.otherOrg), "23503"}, {"actor_scope", fmt.Sprintf("actor_user_id=%d", f.foreign), "23503"}, {"requester_scope", fmt.Sprintf("requested_by=%d", f.foreign), "23503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := f.pool.Exec(f.ctx, "UPDATE knowledge_jobs SET "+tc.expr+" WHERE id=$1", j.ID)
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != tc.code {
				t.Fatalf("want SQLSTATE %s got %v", tc.code, e)
			}
		})
	}
	_, err := f.pool.Exec(f.ctx, `INSERT INTO knowledge_jobs(org_id,document_id,requested_by,actor_user_id,source_sha256) SELECT org_id,document_id,requested_by,actor_user_id,source_sha256 FROM knowledge_jobs WHERE id=$1`, j.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Fatal("dedup constraint missing")
	}
	router := week07Router(f, s)
	dp := f.path(f.org, d.ID) + "/jobs"
	jp := fmt.Sprintf("/api/v1/organizations/%d/jobs/%d", f.org, j.ID)
	ops := []struct{ method, path string }{{"POST", dp}, {"GET", dp}, {"GET", jp}, {"POST", jp + "/retry"}, {"POST", jp + "/cancel"}}
	for _, op := range ops {
		for _, tc := range []struct {
			token, suffix, body string
			status              int
			code                string
		}{{"", "", "", 401, "UNAUTHENTICATED"}, {f.foreignToken, "", "", 404, "NOT_FOUND"}, {f.uploadToken, "?actor_user_id=1", "", 400, "INVALID_REQUEST"}, {f.uploadToken, "", "{}", 400, "INVALID_REQUEST"}, {f.uploadToken, "", `{"requested_by":1}`, 400, "INVALID_REQUEST"}} {
			var body *strings.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			if body == nil {
				knowledgeExpect(t, knowledgeCall(router, tc.token, op.method, op.path+tc.suffix, nil, ""), tc.status, tc.code)
			} else {
				knowledgeExpect(t, knowledgeCall(router, tc.token, op.method, op.path+tc.suffix, body, "application/json"), tc.status, tc.code)
			}
		}
	}
	for _, suffix := range []string{"/retry", "/cancel"} {
		knowledgeExpect(t, knowledgeCall(router, f.peerToken, "POST", jp+suffix, nil, ""), 403, "FORBIDDEN")
	}
	rec := knowledgeCall(router, f.peerToken, "POST", dp, nil, "")
	knowledgeExpect(t, rec, 202, "")
	var dto map[string]any
	knowledgeData(t, rec, &dto)
	allowed := strings.Fields("id org_id document_id requested_by actor_user_id status phase run_version attempts max_attempts total_attempts failure_code retryable last_dispatch_error source_sha256 embedding_model chunker_version created_at updated_at started_at completed_at next_run_at")
	if len(dto) != len(allowed) {
		t.Fatal("job DTO field count changed")
	}
	for _, key := range allowed {
		if _, ok := dto[key]; !ok {
			t.Fatalf("required DTO key missing: %s", key)
		}
	}
	for _, key := range []string{"id", "org_id", "document_id", "requested_by", "actor_user_id", "run_version", "attempts", "max_attempts", "total_attempts"} {
		if _, ok := dto[key].(float64); !ok {
			t.Fatalf("DTO numeric type changed: %s", key)
		}
	}
	for _, key := range []string{"created_at", "updated_at", "next_run_at"} {
		value, ok := dto[key].(string)
		if !ok {
			t.Fatalf("DTO timestamp type changed: %s", key)
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			t.Fatalf("invalid DTO timestamp: %s", key)
		}
	}
	for _, key := range []string{"failure_code", "last_dispatch_error", "started_at", "completed_at"} {
		if dto[key] != nil {
			t.Fatalf("new pending DTO nullable value changed: %s", key)
		}
	}
	if value, ok := dto["retryable"].(bool); !ok || value {
		t.Fatal("pending retryable type/value changed")
	}
	// The caller belongs to the target organization: only the document/job ID is foreign.
	for _, op := range ops {
		foreignPath := strings.Replace(op.path, fmt.Sprintf("/organizations/%d/", f.org), fmt.Sprintf("/organizations/%d/", f.otherOrg), 1)
		knowledgeExpect(t, knowledgeCall(router, f.foreignToken, op.method, foreignPath, nil, ""), 404, "NOT_FOUND")
	}
	disabled := week07Router(f, jobs.NewService(f.pool, f.service, nil, jobs.Limits{}))
	knowledgeExpect(t, knowledgeCall(disabled, f.uploadToken, "POST", dp, nil, ""), 503, "ASYNC_DISABLED")
	knowledgeExpect(t, knowledgeCall(disabled, f.uploadToken, "POST", jp+"/retry", nil, ""), 503, "ASYNC_DISABLED")
	knowledgeExpect(t, knowledgeCall(disabled, f.uploadToken, "GET", jp, nil, ""), 200, "")
	knowledgeExpect(t, knowledgeCall(disabled, f.uploadToken, "GET", dp, nil, ""), 200, "")
	knowledgeExpect(t, knowledgeCall(disabled, f.uploadToken, "POST", jp+"/cancel", nil, ""), 200, "")
	f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.uploader)
	for _, op := range ops {
		knowledgeExpect(t, knowledgeCall(router, f.uploadToken, op.method, op.path, nil, ""), 404, "NOT_FOUND")
	}
	f.exec(t, "UPDATE knowledge_documents SET deleted_at=clock_timestamp() WHERE id=$1", d.ID)
	for _, op := range ops {
		knowledgeExpect(t, knowledgeCall(router, f.adminToken, op.method, op.path, nil, ""), 404, "NOT_FOUND")
	}
}

func TestWeek07DedupPipelineAndRetry(t *testing.T) {
	f := week07Fixture(t)
	e := &week06Embedder{}
	s := week07Service(f, e)
	d, j := week07Submit(t, f, s, "\xef\xbb\xbfOriginal normalized source\r\n")
	var wg sync.WaitGroup
	results := make(chan jobs.Job, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); got, err := s.Submit(f.ctx, f.peer, f.org, d.ID); results <- got; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for got := range results {
		if got.ID != j.ID || got.RequestedBy != f.uploader || got.ActorUserID != f.uploader {
			t.Fatal("concurrent dedup changed identity")
		}
	}
	if j.SourceSHA256 != d.SHA256Hex {
		t.Fatal("job did not snapshot original file")
	}
	_, err := f.service.Process(f.ctx, f.uploader, f.org, d.ID)
	if !errors.Is(err, knowledge.ErrConflict) {
		t.Fatalf("sync process not fenced: %v", err)
	}
	week07Execute(t, f, s, j)
	done := week07Get(t, f, s, j, "SUCCEEDED")
	if done.Attempts != 1 || done.TotalAttempts != 1 || e.calls.Load() != 1 {
		t.Fatal("wrong execution count")
	}
	if f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE document_id=$1 AND content_sha256<>$2", d.ID, j.SourceSHA256) != 1 {
		t.Fatal("source SHA and normalized content SHA conflated")
	}
	for _, p := range []jobs.Payload{week07Payload(j), {JobID: j.ID, OrgID: f.otherOrg, RunVersion: 1}, {JobID: j.ID, OrgID: f.org, RunVersion: 99}} {
		if err := s.Execute(f.ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if e.calls.Load() != 1 {
		t.Fatal("duplicate/foreign/old delivery executed")
	}
	t.Run("backoff_max3_manual_actor", func(t *testing.T) {
		bad := &week06Embedder{fn: func(context.Context, []string) ([][]float32, error) { return nil, rag.NewError("MODEL_UNAVAILABLE") }}
		service := week07Service(f, bad)
		_, job := week07Submit(t, f, service, "Transient retry fixture")
		for attempt := 1; attempt <= 3; attempt++ {
			week07Execute(t, f, service, job)
			status := "RETRYING"
			if attempt == 3 {
				status = "FAILED"
			}
			got := week07Get(t, f, service, job, status)
			if got.Attempts != attempt || got.TotalAttempts != int64(attempt) || got.FailureCode == nil || *got.FailureCode != "MODEL_UNAVAILABLE" {
				t.Fatal("attempt/failure authority lost")
			}
			if attempt < 3 {
				delay := got.NextRunAt.Sub(got.UpdatedAt).Seconds()
				want := 5.0
				if attempt == 2 {
					want = 30
				}
				if delay < want-0.1 || delay > want+0.1 {
					t.Fatalf("backoff %v expected %v", delay, want)
				}
			}
			var group sync.WaitGroup
			for n := 0; n < 8; n++ {
				group.Add(1)
				go func() {
					defer group.Done()
					if err := service.Execute(f.ctx, week07Payload(job)); err != nil {
						t.Error(err)
					}
				}()
			}
			group.Wait()
			if int(bad.calls.Load()) != attempt {
				t.Fatal("duplicate delivery bypassed due/max attempts")
			}
			if attempt < 3 {
				f.exec(t, "UPDATE knowledge_jobs SET next_run_at=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID)
			}
		}
		dedup, err := service.Submit(f.ctx, f.uploader, f.org, job.DocumentID)
		if err != nil || dedup.Status != "FAILED" || dedup.Attempts != 3 {
			t.Fatal("terminal submit reset job")
		}
		retry, err := service.Retry(f.ctx, f.admin, f.org, job.ID)
		if err != nil || retry.RunVersion != 2 || retry.Attempts != 0 || retry.TotalAttempts != 3 || retry.RequestedBy != f.uploader || retry.ActorUserID != f.admin {
			t.Fatal("manual retry provenance/counters wrong")
		}
		week07Execute(t, f, service, job)
		if bad.calls.Load() != 3 {
			t.Fatal("old run executed")
		}
		if _, err = service.Cancel(f.ctx, f.admin, f.org, job.ID); err != nil {
			t.Fatal(err)
		}
		dedup, err = service.Submit(f.ctx, f.uploader, f.org, job.DocumentID)
		if err != nil || dedup.Status != "CANCELED" {
			t.Fatal("canceled submit reset job")
		}
	})
}

func TestWeek07PermanentFailures(t *testing.T) {
	for _, name := range []string{"pdf", "invalid_vector", "tampered_file", "source_hash", "authorization"} {
		t.Run(name, func(t *testing.T) {
			f := week07Fixture(t)
			e := &week06Embedder{}
			s := week07Service(f, e)
			var d knowledge.Document
			var j jobs.Job
			var err error
			want := ""
			if name == "pdf" {
				d = f.upload(t, f.uploadToken, f.org, "synthetic.pdf", []byte("%PDF-1.7\nsynthetic unsupported file\n"))
				j, err = s.Submit(f.ctx, f.uploader, f.org, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				want = "PDF_UNSUPPORTED"
			} else {
				d, j = week07Submit(t, f, s, "original fixture")
			}
			switch name {
			case "invalid_vector":
				e.fn = func(_ context.Context, texts []string) ([][]float32, error) {
					v := week06Vectors(len(texts))
					v[0] = []float32{1}
					return v, nil
				}
				want = "EMBEDDING_DIMENSION_MISMATCH"
			case "tampered_file":
				if os.WriteFile(filepath.Join(f.dir, f.key(t, d)), []byte("modified fixture"), 0600) != nil {
					t.Fatal("cannot modify synthetic source")
				}
				want = "SOURCE_CHANGED"
			case "source_hash":
				f.exec(t, "UPDATE knowledge_documents SET sha256_hex=$2 WHERE id=$1", d.ID, strings.Repeat("a", 64))
				want = "SOURCE_CHANGED"
			case "authorization":
				f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.uploader)
				want = "AUTHORIZATION_REVOKED"
			}
			week07Execute(t, f, s, j)
			got := week07Get(t, f, s, j, "FAILED")
			if got.Retryable || got.FailureCode == nil || *got.FailureCode != want {
				t.Fatalf("permanent error=%v retryable=%v expected=%s", got.FailureCode, got.Retryable, want)
			}
			if f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE document_id=$1", d.ID) != 0 {
				t.Fatal("permanent failure published chunks")
			}
		})
	}
}

func TestWeek07DispatchReservations(t *testing.T) {
	f := week07Fixture(t)
	s := week07Service(f, &week06Embedder{})
	_, j := week07Submit(t, f, s, "Durable dispatch fixture")
	calls := 0
	lost := week07Queue(func(ctx context.Context, p jobs.Payload, seq int64) error {
		if p.JobID != j.ID {
			return nil
		}
		calls++
		// Independent connection proves commit precedes queue I/O and no job lock remains.
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		_, err = tx.Exec(ctx, "SELECT id FROM knowledge_jobs WHERE id=$1 FOR UPDATE NOWAIT", j.ID)
		if err != nil {
			t.Error("enqueue held transaction lock")
		}
		if seq != int64(calls) {
			t.Error("wrong reservation sequence")
		}
		return errors.New("synthetic transport loss")
	})
	if jobs.Code(s.Dispatch(f.ctx, lost)) != "DELIVERY_RETRY" {
		t.Fatal("send failure acknowledged")
	}
	got := week07Get(t, f, s, j, "PENDING")
	if got.LastDispatchError == nil || *got.LastDispatchError != "QUEUE_UNAVAILABLE" || got.Attempts != 0 {
		t.Fatal("queue loss destroyed durable intent")
	}
	if err := s.Dispatch(f.ctx, lost); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("15 second reservation ignored")
	}
	f.exec(t, "UPDATE knowledge_jobs SET next_dispatch_at=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
	_ = s.Dispatch(f.ctx, lost)
	if calls != 2 {
		t.Fatal("lost delivery was not redispatched")
	}
	f.exec(t, "UPDATE knowledge_jobs SET next_dispatch_at=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
	race := week07Queue(func(ctx context.Context, p jobs.Payload, seq int64) error {
		if p.JobID != j.ID {
			return nil
		}
		if _, err := s.Cancel(ctx, f.uploader, f.org, j.ID); err != nil {
			t.Error(err)
		}
		if _, err := s.Retry(ctx, f.admin, f.org, j.ID); err != nil {
			t.Error(err)
		}
		return errors.New("old dispatch response")
	})
	_ = s.Dispatch(f.ctx, race)
	got = week07Get(t, f, s, j, "PENDING")
	if got.RunVersion != 2 || got.LastDispatchError != nil {
		t.Fatal("stale dispatch response corrupted new run")
	}
	// A newer reservation in the SAME run must also fence a late send result.
	first := true
	seqRace := week07Queue(func(ctx context.Context, p jobs.Payload, seq int64) error {
		if p.JobID == j.ID && first {
			first = false
			f.exec(t, "UPDATE knowledge_jobs SET next_dispatch_at=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
			if err := s.Dispatch(ctx, week07Discard); err != nil {
				t.Error(err)
			}
			return errors.New("late earlier sequence")
		}
		return nil
	})
	_ = s.Dispatch(f.ctx, seqRace)
	got = week07Get(t, f, s, j, "PENDING")
	if got.LastDispatchError != nil {
		t.Fatal("stale dispatch sequence overwrote successful reservation")
	}
}

func TestWeek07BlockedPublicationAndOldWorker(t *testing.T) {
	for _, mutation := range []string{"cancel_retry", "revoke", "source", "delete", "expire"} {
		t.Run(mutation, func(t *testing.T) {
			f := week07Fixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			e := &week06Embedder{fn: func(_ context.Context, texts []string) ([][]float32, error) {
				close(entered)
				<-release
				return week06Vectors(len(texts)), nil
			}}
			s := week07Service(f, e)
			d, j := week07Submit(t, f, s, "Blocked publication fixture")
			done := make(chan error, 1)
			go func() { done <- s.Execute(f.ctx, week07Payload(j)) }()
			week06Await(t, entered)
			_, err := week06Service(f, &week06Embedder{}, &week06Generator{}).Index(f.ctx, f.peer, f.org, d.ID)
			week06Error(t, err, "INDEX_STATE_CONFLICT")
			if err := s.Execute(f.ctx, week07Payload(j)); err != nil {
				t.Fatal(err)
			}
			if e.calls.Load() != 1 {
				t.Fatal("duplicate stole live lease")
			}
			want := "FAILED"
			switch mutation {
			case "cancel_retry":
				if _, err = s.Cancel(f.ctx, f.uploader, f.org, j.ID); err != nil {
					t.Fatal(err)
				}
				next, err := s.Retry(f.ctx, f.admin, f.org, j.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh := week07Service(f, &week06Embedder{})
				week07Execute(t, f, fresh, next)
				week07Get(t, f, fresh, next, "SUCCEEDED")
				want = "SUCCEEDED"
			case "revoke":
				f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.uploader)
			case "source":
				f.exec(t, "UPDATE knowledge_documents SET sha256_hex=$2 WHERE id=$1", d.ID, strings.Repeat("c", 64))
			case "delete":
				f.exec(t, "UPDATE knowledge_documents SET deleted_at=clock_timestamp() WHERE id=$1", d.ID)
				want = "CANCELED"
			case "expire":
				f.exec(t, "UPDATE knowledge_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
				// Let at least one real heartbeat run; a revoked lease cannot be extended.
				time.Sleep(250 * time.Millisecond)
				if f.count(t, "SELECT count(*) FROM knowledge_jobs WHERE id=$1 AND lease_until<=clock_timestamp()", j.ID) != 1 {
					t.Fatal("heartbeat resurrected expired lease")
				}
			}
			once.Do(func() { close(release) })
			err = week06Await(t, done)
			if mutation == "expire" {
				if jobs.Code(err) != "DELIVERY_RETRY" {
					t.Fatal("unknown expired outcome was acknowledged")
				}
				if err = s.Dispatch(f.ctx, week07Discard); err != nil {
					t.Fatal(err)
				}
				want = "RETRYING"
			} else if err != nil {
				t.Fatal(err)
			}
			var status string
			if err = f.pool.QueryRow(f.ctx, "SELECT status FROM knowledge_jobs WHERE id=$1", j.ID).Scan(&status); err != nil || status != want {
				t.Fatalf("final status=%s want=%s err=%v", status, want, err)
			}
			count := f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE document_id=$1", d.ID)
			if mutation == "cancel_retry" {
				if count != 1 || f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE document_id=$1 AND status='READY'", d.ID) != 1 {
					t.Fatal("old worker damaged new publication")
				}
			} else if count != 0 {
				t.Fatal("revoked worker published late chunks")
			}
			if f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE document_id=$1 AND status='INDEXING'", d.ID) != 0 {
				t.Fatal("ended owned INDEXING row was not cleaned")
			}
			if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE id=$1 AND status='READY'", d.ID) != 1 {
				t.Fatal("cleanup downgraded completed text phase")
			}
		})
	}
}

func TestWeek07RecoveryStateInjection(t *testing.T) {
	for _, mode := range []string{"ready_attempt3", "unfinished_attempt3", "ready_revoked", "ready_source", "legacy_processing"} {
		t.Run(mode, func(t *testing.T) {
			f := week07Fixture(t)
			e := &week06Embedder{}
			s := week07Service(f, e)
			d, j := week07Submit(t, f, s, "Recovery source\r\n")
			if strings.HasPrefix(mode, "ready_") {
				week07Execute(t, f, s, j)
			}
			// Simulate persisted state after unknown acknowledgement; process tests separately kill a real PID.
			f.exec(t, `UPDATE knowledge_jobs SET status='RUNNING',attempts=3,total_attempts=3,completed_at=NULL,lease_token=$2,lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, j.ID, strings.Repeat("b", 32))
			want := "FAILED"
			switch mode {
			case "ready_attempt3":
				want = "SUCCEEDED"
			case "ready_revoked":
				f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.uploader)
			case "ready_source":
				f.exec(t, "UPDATE knowledge_documents SET sha256_hex=$2 WHERE id=$1", d.ID, strings.Repeat("c", 64))
			case "legacy_processing":
				f.exec(t, "UPDATE knowledge_documents SET status='PROCESSING',task_token=NULL WHERE id=$1", d.ID)
			}
			before := e.calls.Load()
			if err := s.Dispatch(f.ctx, week07Discard); err != nil {
				t.Fatal(err)
			}
			got := week07Get(t, f, s, j, want)
			week07Execute(t, f, s, j)
			if e.calls.Load() != before || got.Attempts != 3 || got.TotalAttempts != 3 {
				t.Fatal("recovery performed fourth attempt")
			}
			if mode == "legacy_processing" && f.count(t, "SELECT count(*) FROM knowledge_documents WHERE id=$1 AND status='PROCESSING' AND task_token IS NULL", d.ID) != 1 {
				t.Fatal("recovery reset unowned legacy processing")
			}
			if mode == "ready_revoked" && (got.FailureCode == nil || *got.FailureCode != "AUTHORIZATION_REVOKED") {
				t.Fatal("recovery success bypassed authorization")
			}
			if mode == "ready_source" && (got.FailureCode == nil || *got.FailureCode != "SOURCE_CHANGED") {
				t.Fatal("recovery success bypassed source fingerprint")
			}
			if strings.HasPrefix(mode, "ready_") && f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE document_id=$1 AND status='READY'", d.ID) != 1 {
				t.Fatal("recovery downgraded READY")
			}
		})
	}
}

func TestWeek07RealRedisDelivery(t *testing.T) {
	f := week07Fixture(t)
	host, _, err := net.SplitHostPort(os.Getenv("REDIS_ADDR"))
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || os.Getenv("REDIS_DB") != "6" || os.Getenv("REDIS_PASSWORD") != "" {
		t.Fatal("requires owned loopback Redis DB 6")
	}
	cfg := config.AsyncConfig{Enabled: true, RedisAddr: os.Getenv("REDIS_ADDR"), RedisDB: 6, Concurrency: 2}
	client := taskqueue.NewClient(cfg)
	defer client.Close()
	if err = client.Ping(f.ctx); err != nil {
		t.Fatal(err)
	}
	e := &week06Embedder{}
	s := week07Service(f, e)
	d, j := week07Submit(t, f, s, "Real Redis queue fixture")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	server := taskqueue.NewServer(ctx, cfg, s.Execute)
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()
	if err = s.Dispatch(f.ctx, client); err != nil {
		t.Fatal(err)
	}
	// Duplicate transport deliveries retain DB-owned single execution.
	if err = client.Enqueue(f.ctx, week07Payload(j), 100001); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		got, err := s.Get(f.ctx, f.admin, f.org, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real Redis Worker failed to complete")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if e.calls.Load() != 1 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE document_id=$1", d.ID) != 1 {
		t.Fatal("transport duplicate executed twice")
	}
	server.Stop()
	cancel()
}

func TestWeek07ClaimLockTimeout(t *testing.T) {
	f := week07Fixture(t)
	s := week07Service(f, &week06Embedder{})
	_, j := week07Submit(t, f, s, "Claim lock timeout fixture")
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(f.ctx, "SELECT id FROM knowledge_documents WHERE id=$1 FOR UPDATE", j.DocumentID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 75*time.Millisecond)
	defer cancel()
	if jobs.Code(s.Execute(ctx, week07Payload(j))) != "DELIVERY_RETRY" {
		t.Fatal("claim lock timeout acknowledged")
	}
	if err = tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	week07Get(t, f, s, j, "PENDING")
	week07Execute(t, f, s, j)
	week07Get(t, f, s, j, "SUCCEEDED")
}

func TestWeek07ConcurrentFirstSubmit(t *testing.T) {
	f := week07Fixture(t)
	s := week07Service(f, &week06Embedder{})
	d := f.upload(t, f.uploadToken, f.org, "first-submit.md", []byte("No existing job before concurrent submissions"))
	if f.count(t, "SELECT count(*) FROM knowledge_jobs WHERE document_id=$1", d.ID) != 0 {
		t.Fatal("first-insert fixture already has a job")
	}
	start := make(chan struct{})
	results := make(chan jobs.Job, 12)
	errs := make(chan error, 12)
	var wg, ready sync.WaitGroup
	ready.Add(12)
	for n := 0; n < 12; n++ {
		actor := f.uploader
		if n%2 == 1 {
			actor = f.peer
		}
		wg.Add(1)
		go func(actor int64) {
			defer wg.Done()
			ready.Done()
			<-start
			j, err := s.Submit(f.ctx, actor, f.org, d.ID)
			results <- j
			errs <- err
		}(actor)
	}
	ready.Wait()
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var winner jobs.Job
	for j := range results {
		if winner.ID == 0 {
			winner = j
		}
		if j.ID != winner.ID || j.RequestedBy != winner.RequestedBy || j.ActorUserID != winner.ActorUserID {
			t.Fatal("first-insert concurrent dedup changed identity")
		}
	}
	if (winner.RequestedBy != f.uploader && winner.RequestedBy != f.peer) || winner.ActorUserID != winner.RequestedBy {
		t.Fatal("first-insert winner provenance forged")
	}
	if f.count(t, "SELECT count(*) FROM knowledge_jobs WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 1 {
		t.Fatal("concurrent first submit created multiple rows")
	}
}

type week07Extractor func(context.Context, io.Reader, string) (knowledge.ExtractedText, error)

func (x week07Extractor) Extract(ctx context.Context, r io.Reader, media string) (knowledge.ExtractedText, error) {
	return x(ctx, r, media)
}

// Deliberately noncooperative external extraction returns after cancellation;
// production service/repository must still fence publication and failure cleanup.
func week07BlockedExtractor(entered, release chan struct{}) knowledge.TextExtractor {
	return week07Extractor(func(ctx context.Context, r io.Reader, media string) (knowledge.ExtractedText, error) {
		result, err := knowledge.NewTextExtractor(5<<20, 1<<20).Extract(context.WithoutCancel(ctx), r, media)
		close(entered)
		<-release
		return result, err
	})
}

func TestWeek07OwnedProcessingFences(t *testing.T) {
	for _, mode := range []string{"cancel_retry", "expire"} {
		t.Run(mode, func(t *testing.T) {
			f := week07Fixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			f.service = f.newService(week07BlockedExtractor(entered, release), f.limits)
			s := week07Service(f, &week06Embedder{})
			d, j := week07Submit(t, f, s, "Owned PROCESSING barrier fixture")
			done := make(chan error, 1)
			go func() { done <- s.Execute(f.ctx, week07Payload(j)) }()
			week06Await(t, entered)
			var oldToken string
			if err := f.pool.QueryRow(f.ctx, "SELECT task_token FROM knowledge_documents WHERE id=$1 AND status='PROCESSING'", d.ID).Scan(&oldToken); err != nil || oldToken == "" {
				t.Fatal("extractor did not hold owned PROCESSING")
			}
			if f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE document_id=$1", d.ID) != 0 {
				t.Fatal("blocked extraction published text")
			}
			if mode == "expire" {
				f.exec(t, "UPDATE knowledge_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID)
				once.Do(func() { close(release) })
				if jobs.Code(week06Await(t, done)) != "DELIVERY_RETRY" {
					t.Fatal("expired extraction acknowledged unknown finish")
				}
				if err := s.Dispatch(f.ctx, week07Discard); err != nil {
					t.Fatal(err)
				}
				week07Get(t, f, s, j, "RETRYING")
				if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE id=$1 AND status='FAILED' AND task_token IS NULL", d.ID) != 1 || f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE document_id=$1", d.ID) != 0 {
					t.Fatal("expired owned extraction not cleaned without publication")
				}
				return
			}
			if _, err := s.Cancel(f.ctx, f.uploader, f.org, j.ID); err != nil {
				t.Fatal(err)
			}
			if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE id=$1 AND status='FAILED' AND task_token IS NULL", d.ID) != 1 {
				t.Fatal("cancel failed to clean owned PROCESSING")
			}
			next, err := s.Retry(f.ctx, f.admin, f.org, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			newEntered, newRelease := make(chan struct{}), make(chan struct{})
			var newOnce sync.Once
			defer newOnce.Do(func() { close(newRelease) })
			f.service = f.newService(week07BlockedExtractor(newEntered, newRelease), f.limits)
			fresh := week07Service(f, &week06Embedder{})
			newDone := make(chan error, 1)
			go func() { newDone <- fresh.Execute(f.ctx, week07Payload(next)) }()
			week06Await(t, newEntered)
			var newToken string
			if err = f.pool.QueryRow(f.ctx, "SELECT task_token FROM knowledge_documents WHERE id=$1 AND status='PROCESSING'", d.ID).Scan(&newToken); err != nil || newToken == oldToken || newToken == "" {
				t.Fatal("replacement extraction did not receive a new owner")
			}
			once.Do(func() { close(release) })
			if err = week06Await(t, done); err != nil {
				t.Fatal(err)
			}
			if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE id=$1 AND status='PROCESSING' AND task_token=$2", d.ID, newToken) != 1 || f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE document_id=$1", d.ID) != 0 {
				t.Fatal("old extractor published or reset replacement ownership")
			}
			newOnce.Do(func() { close(newRelease) })
			if err = week06Await(t, newDone); err != nil {
				t.Fatal(err)
			}
			week07Get(t, f, fresh, next, "SUCCEEDED")
			if f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE document_id=$1", d.ID) != 1 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE document_id=$1", d.ID) != 1 {
				t.Fatal("replacement did not publish exactly once")
			}
		})
	}
}

func TestWeek07OldWorkerWhileReplacementIndexing(t *testing.T) {
	f := week07Fixture(t)
	oldEntered, oldRelease := make(chan struct{}), make(chan struct{})
	var oldOnce sync.Once
	defer oldOnce.Do(func() { close(oldRelease) })
	oldModel := &week06Embedder{fn: func(_ context.Context, texts []string) ([][]float32, error) {
		close(oldEntered)
		<-oldRelease
		return week06Vectors(len(texts)), nil
	}}
	s := week07Service(f, oldModel)
	d, j := week07Submit(t, f, s, "Replacement remains INDEXING fixture")
	oldDone := make(chan error, 1)
	go func() { oldDone <- s.Execute(f.ctx, week07Payload(j)) }()
	week06Await(t, oldEntered)
	var oldToken string
	if err := f.pool.QueryRow(f.ctx, "SELECT task_token FROM knowledge_document_indexes WHERE document_id=$1", d.ID).Scan(&oldToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel(f.ctx, f.uploader, f.org, j.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.Retry(f.ctx, f.admin, f.org, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	newEntered, newRelease := make(chan struct{}), make(chan struct{})
	var newOnce sync.Once
	defer newOnce.Do(func() { close(newRelease) })
	newModel := &week06Embedder{fn: func(_ context.Context, texts []string) ([][]float32, error) {
		close(newEntered)
		<-newRelease
		return week06Vectors(len(texts)), nil
	}}
	fresh := week07Service(f, newModel)
	newDone := make(chan error, 1)
	go func() { newDone <- fresh.Execute(f.ctx, week07Payload(next)) }()
	week06Await(t, newEntered)
	var newToken string
	if err = f.pool.QueryRow(f.ctx, "SELECT task_token FROM knowledge_document_indexes WHERE document_id=$1 AND status='INDEXING'", d.ID).Scan(&newToken); err != nil || newToken == oldToken || newToken == "" {
		t.Fatal("replacement index ownership missing")
	}
	oldOnce.Do(func() { close(oldRelease) })
	if err = week06Await(t, oldDone); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "SELECT count(*) FROM knowledge_document_indexes WHERE document_id=$1 AND status='INDEXING' AND task_token=$2", d.ID, newToken) != 1 || f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE document_id=$1", d.ID) != 0 {
		t.Fatal("old worker published or reset replacement INDEXING")
	}
	if got := week07Get(t, f, fresh, next, "RUNNING"); got.RunVersion != 2 || got.Attempts != 1 {
		t.Fatal("old worker changed replacement job")
	}
	newOnce.Do(func() { close(newRelease) })
	if err = week06Await(t, newDone); err != nil {
		t.Fatal(err)
	}
	week07Get(t, f, fresh, next, "SUCCEEDED")
	if f.count(t, "SELECT count(*) FROM knowledge_chunks WHERE document_id=$1", d.ID) != 1 {
		t.Fatal("replacement chunk publication duplicated")
	}
}

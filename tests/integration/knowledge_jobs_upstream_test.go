package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/platform/llm"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

// This is an adapter-to-job failure contract, not a semantic model evaluation.
// PostgreSQL and all domain services are real. Only the external HTTP model
// response is controlled; fixture due-time advancement avoids a 35-second wait.
func TestWeek07ModelHTTPFailureRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		status    int
		code      string
		transient bool
	}{
		{400, "MODEL_UPSTREAM_ERROR", false},
		{401, "MODEL_UPSTREAM_ERROR", false},
		{403, "MODEL_UPSTREAM_ERROR", false},
		{422, "MODEL_UPSTREAM_ERROR", false},
		{500, "MODEL_UPSTREAM_ERROR", false},
		{429, "MODEL_RATE_LIMITED", true},
		{503, "MODEL_UNAVAILABLE", true},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			f := week07Fixture(t)
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
					t.Errorf("unexpected model request %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("synthetic private upstream response"))
			}))
			defer server.Close()
			client, err := llm.New(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			indexer := rag.NewService(f.pool, f.service, client.Embedder(), client.Generator(), rag.Limits{Enabled: true, MinimumScore: 0.4})
			service := jobs.NewService(f.pool, f.service, indexer, jobs.Limits{Enabled: true, HeartbeatInterval: 100 * time.Millisecond})
			d, j := week07Submit(t, f, service, "Adapter to durable job policy fixture")
			maximum := 1
			if tc.transient {
				maximum = 3
			}
			for attempt := 1; attempt <= maximum; attempt++ {
				week07Execute(t, f, service, j)
				status := "FAILED"
				if tc.transient && attempt < maximum {
					status = "RETRYING"
				}
				got := week07Get(t, f, service, j, status)
				if got.Attempts != attempt || got.TotalAttempts != int64(attempt) || got.MaxAttempts != 3 || got.FailureCode == nil || *got.FailureCode != tc.code || got.Retryable != tc.transient {
					t.Fatalf("HTTP %d attempt %d: attempts=%d total=%d max=%d code=%v retryable=%v", tc.status, attempt, got.Attempts, got.TotalAttempts, got.MaxAttempts, got.FailureCode, got.Retryable)
				}
				if calls.Load() != int64(attempt) {
					t.Fatalf("HTTP %d model calls=%d want=%d", tc.status, calls.Load(), attempt)
				}
				if status == "RETRYING" {
					want := 5 * time.Second
					if attempt == 2 {
						want = 30 * time.Second
					}
					actual := got.NextRunAt.Sub(got.UpdatedAt)
					if actual < want-100*time.Millisecond || actual > want+100*time.Millisecond {
						t.Fatalf("HTTP %d backoff=%v want=%v", tc.status, actual, want)
					}
				}
				// Both not-due retries and terminal deliveries must be known no-ops.
				week07Execute(t, f, service, j)
				after := week07Get(t, f, service, j, status)
				if calls.Load() != int64(attempt) || after.Attempts != attempt || after.TotalAttempts != int64(attempt) {
					t.Fatal("redelivery bypassed backoff or terminal state")
				}
				if status == "RETRYING" {
					f.exec(t, `UPDATE knowledge_jobs SET next_run_at=clock_timestamp()-interval '1 second' WHERE org_id=$1 AND id=$2`, f.org, j.ID)
				}
			}
			if f.count(t, `SELECT count(*) FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2`, f.org, d.ID) != 0 {
				t.Fatal("model failure published chunks")
			}
			if f.count(t, `SELECT count(*) FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 AND status='FAILED' AND task_token IS NULL`, f.org, d.ID) != 1 {
				t.Fatal("model failure did not release index ownership")
			}
		})
	}
}

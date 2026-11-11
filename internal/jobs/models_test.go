package jobs

import (
	"context"
	"errors"
	"fmt"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/llm"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPayloadStrictCanonicalIdentity(t *testing.T) {
	valid := `{"job_id":9223372036854775807,"org_id":2,"run_version":3}`
	p, e := ParsePayload([]byte(valid))
	if e != nil || p.JobID != 9223372036854775807 {
		t.Fatalf("valid identity: %+v %v", p, e)
	}
	for _, s := range []string{`{}`, `null`, `[]`, valid + ` {}`, strings.Repeat(" ", 1025), `{"job_id":1,"job_id":2,"org_id":2,"run_version":3}`, `{"job_id":1,"org_id":2,"run_version":3,"actor":4}`, `{"job_id":1e0,"org_id":2,"run_version":3}`, `{"job_id":1.0,"org_id":2,"run_version":3}`, `{"job_id":"1","org_id":2,"run_version":3}`, `{"job_id":0,"org_id":2,"run_version":3}`, `{"job_id":-1,"org_id":2,"run_version":3}`, `{"job_id":9223372036854775808,"org_id":2,"run_version":3}`} {
		if _, e := ParsePayload([]byte(s)); Code(e) != "INVALID_REQUEST" {
			t.Errorf("accepted malformed %q: %v", s, e)
		}
	}
}
func TestFailureClassificationAndBackoff(t *testing.T) {
	cases := []struct {
		e     error
		code  string
		retry bool
	}{
		{knowledge.ErrInvalidRequest, "INVALID_REQUEST", false}, {knowledge.ErrUnauthenticated, "AUTHORIZATION_REVOKED", false}, {rag.NewError("NOT_FOUND"), "AUTHORIZATION_REVOKED", false}, {knowledge.ErrSourceChanged, "SOURCE_CHANGED", false}, {knowledge.ErrStorage, "STORAGE_ERROR", true}, {context.DeadlineExceeded, "TASK_TIMEOUT", true}, {knowledge.ErrNotFound, "AUTHORIZATION_REVOKED", false},
		{&knowledge.ExtractionError{Code: "PDF_UNSUPPORTED"}, "PDF_UNSUPPORTED", false}, {rag.NewError("EMBEDDING_DIMENSION_MISMATCH"), "EMBEDDING_DIMENSION_MISMATCH", false}, {rag.NewError("MODEL_UNAVAILABLE"), "MODEL_UNAVAILABLE", true}, {errors.New("secret underlying cause"), "STORAGE_ERROR", true},
	}
	for _, c := range cases {
		code, retry := classify(c.e)
		if code != c.code || retry != c.retry {
			t.Errorf("%v: %s %v", c.e, code, retry)
		}
	}
	if backoff(1) != 5*time.Second || backoff(2) != 30*time.Second || backoff(3) != 30*time.Second {
		t.Fatal("backoff contract")
	}
	if !errors.Is(knowledge.ErrSourceChanged, knowledge.ErrStorage) {
		t.Fatal("source sentinel broke old storage contract")
	}
}

// Real adapter status mapping, followed by the job policy: an ambiguous safe
// code is not proof of transience. No upstream response text is inspected.
func TestModelHTTPFailurePolicyKnownTransience(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		retry  bool
	}{
		{400, "MODEL_UPSTREAM_ERROR", false}, {401, "MODEL_UPSTREAM_ERROR", false}, {403, "MODEL_UPSTREAM_ERROR", false}, {422, "MODEL_UPSTREAM_ERROR", false}, {500, "MODEL_UPSTREAM_ERROR", false},
		{429, "MODEL_RATE_LIMITED", true}, {503, "MODEL_UNAVAILABLE", true}, {408, "MODEL_TIMEOUT", true}, {504, "MODEL_TIMEOUT", true},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("private upstream body must never escape"))
			}))
			defer server.Close()
			client, e := llm.New(server.URL)
			if e != nil {
				t.Fatal(e)
			}
			_, e = client.Embedder().Embed(context.Background(), []string{"synthetic input"})
			if e == nil {
				t.Fatal("error status accepted")
			}
			code, retry := classify(e)
			if code != tc.code || retry != tc.retry {
				t.Fatalf("HTTP %d: code=%s retry=%v, want code=%s retry=%v", tc.status, code, retry, tc.code, tc.retry)
			}
			if strings.Contains(e.Error(), "private") {
				t.Fatal("upstream body exposed")
			}
		})
	}
}

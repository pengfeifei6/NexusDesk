package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

type deadlineTransport struct {
	next    http.RoundTripper
	t       *testing.T
	maximum time.Duration
}

func (d deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	d.t.Helper()
	deadline, ok := r.Context().Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining <= d.maximum-time.Second || remaining > d.maximum {
		d.t.Errorf("deadline remaining %v, want approximately %v", remaining, d.maximum)
	}
	return d.next.RoundTrip(r)
}

func TestAdapterDeadlinesBoundActualHTTPRequests(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer s.Close()
	for _, tc := range []struct {
		path   string
		budget time.Duration
	}{{"/api/embed", 20 * time.Second}, {"/api/chat", 35 * time.Second}} {
		c, err := New(s.URL)
		if err != nil {
			t.Fatal(err)
		}
		c.http.Transport = deadlineTransport{next: c.http.Transport, t: t, maximum: tc.budget}
		if tc.path == "/api/embed" {
			_, err = c.Embedder().Embed(context.Background(), []string{"sample"})
		} else {
			_, err = c.Generator().Generate(context.Background(), "question", []rag.Source{{ChunkID: 1, Excerpt: "evidence"}})
		}
		var modelError *rag.Error
		if !errors.As(err, &modelError) || modelError.Code != "MODEL_INVALID_RESPONSE" {
			t.Fatalf("expected parsed fixture response, got %v", err)
		}
	}
}

package ai

import (
	"bytes"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type plainWriter struct {
	header http.Header
	bytes.Buffer
}

func (w *plainWriter) Header() http.Header { return w.header }
func (w *plainWriter) WriteHeader(int)     {}
func TestSSEFlushAndFraming(t *testing.T) {
	w := httptest.NewRecorder()
	s, err := newSSE(w)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Send("content_delta", map[string]string{"delta": "中文\n\nevent: evil"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Ping(); err != nil {
		t.Fatal(err)
	}
	if !w.Flushed || w.Header().Get("X-Accel-Buffering") != "no" || !strings.Contains(w.Header().Get("Cache-Control"), "no-transform") {
		t.Fatal("missing flush/headers")
	}
	if bytes.Count(w.Body.Bytes(), []byte("event:")) != 2 || !strings.HasSuffix(w.Body.String(), ": ping\n\n") {
		t.Fatal("bad framing")
	}
	if strings.Contains(w.Body.String(), "\n\nevent: evil") {
		t.Fatal("event injection")
	}
}
func TestSSERejectsUnsupportedWriterBeforeHeaders(t *testing.T) {
	w := &plainWriter{header: make(http.Header)}
	if _, err := newSSE(w); err == nil {
		t.Fatal("writer accepted without Flush")
	}
	if w.Len() != 0 {
		t.Fatal("committed before validating Flush")
	}
}

type failedFlushWriter struct{ plainWriter }

var flushFailure = errors.New("flush failed")

func (w *failedFlushWriter) Flush()            {}
func (w *failedFlushWriter) FlushError() error { return flushFailure }
func TestSSEPreservesUnderlyingFlushErrorThroughGin(t *testing.T) {
	w := &failedFlushWriter{plainWriter: plainWriter{header: make(http.Header)}}
	c, _ := gin.CreateTestContext(w)
	s, err := newSSE(c.Writer)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Send("content_delta", map[string]string{"delta": "x"}); !errors.Is(err, flushFailure) {
		t.Fatalf("lost FlushError through Gin: %v", err)
	}
}

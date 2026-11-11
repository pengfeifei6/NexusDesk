package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type sseWriter struct {
	writer     http.ResponseWriter
	controller *http.ResponseController
}

func newSSE(w http.ResponseWriter) (*sseWriter, error) {
	// Gin implements Flush even when its underlying writer does not. Inspect
	// all wrappers before committing headers, avoiding Gin's type-assert panic.
	underlying := w
	for {
		u, ok := underlying.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		underlying = u.Unwrap()
	}
	if _, ok := underlying.(http.Flusher); !ok {
		return nil, NewError("INTERNAL_ERROR")
	}
	return &sseWriter{writer: w, controller: http.NewResponseController(underlying)}, nil
}
func (s *sseWriter) write(body string) error {
	s.writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	s.writer.Header().Set("Cache-Control", "no-cache, no-store, no-transform")
	s.writer.Header().Set("X-Accel-Buffering", "no")
	// Network servers support write deadlines; in-memory httptest writers do not.
	_ = s.controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(s.writer, body); err != nil {
		return err
	}
	return s.controller.Flush()
}
func (s *sseWriter) Send(event string, data any) error {
	switch event {
	case "message_start", "content_delta", "citation", "tool_approval_required", "message_end", "error":
	default:
		return NewError("INTERNAL_ERROR")
	}
	b, err := json.Marshal(data)
	if err != nil {
		return NewError("INTERNAL_ERROR")
	}
	return s.write("event: " + event + "\ndata: " + string(b) + "\n\n")
}
func (s *sseWriter) Ping() error { return s.write(": ping\n\n") }

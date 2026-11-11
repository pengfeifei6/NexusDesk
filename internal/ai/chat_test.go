package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/ai/model"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

type completionFixture struct {
	status, content string
	ctxErr          error
	fail            bool
}

func (f *completionFixture) FinishTurn(ctx context.Context, a Actor, id, status, content string, s []rag.Source, in, out int64) error {
	f.status = status
	f.content = content
	f.ctxErr = ctx.Err()
	if f.fail {
		return NewError("INTERNAL_ERROR")
	}
	return nil
}
func TestChatCanceledCleanupAndFailure(t *testing.T) {
	for _, kind := range []string{"cancel", "timeout", "upstream", "database"} {
		t.Run(kind, func(t *testing.T) {
			f := &toolFixture{}
			p := &completionFixture{fail: kind == "database"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m := modelFunc(func(c context.Context, r model.ChatRequest, e func(model.ModelEvent) error) error {
				if err := e(model.ModelEvent{Kind: model.TextDelta, Delta: "partial"}); err != nil {
					return err
				}
				switch kind {
				case "cancel":
					cancel()
					return c.Err()
				case "timeout":
					return context.DeadlineExceeded
				case "upstream":
					return errors.New("private provider error")
				}
				return e(model.ModelEvent{Kind: model.Completed})
			})
			s := &ChatService{runner: NewRunner(m, f, f, f), completion: p}
			_, err := s.generate(ctx, Actor{1, 2}, runTurn(), func(string, any) error { return nil })
			if err == nil || p.ctxErr != nil || p.content != "partial" {
				t.Fatalf("cleanup status=%s err=%v context=%v", p.status, err, p.ctxErr)
			}
			if kind == "cancel" && p.status != "INTERRUPTED" {
				t.Fatal("cancel status")
			}
			if kind == "upstream" && Code(err) != "INTERNAL_ERROR" {
				t.Fatal("unsafe error")
			}
		})
	}
}
func TestChatConcurrencyBound(t *testing.T) {
	s := NewChatService(nil, nil)
	release, e := s.acquire()
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	r2, e := s.acquire()
	if e != nil {
		t.Fatal(e)
	}
	defer r2()
	start := time.Now()
	if _, e = s.acquire(); Code(e) != "AI_BUSY" || time.Since(start) > time.Second {
		t.Fatal("unbounded wait")
	}
}

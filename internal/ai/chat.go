package ai

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

type completionStore interface {
	FinishTurn(context.Context, Actor, string, string, string, []rag.Source, int64, int64) error
}
type ChatService struct {
	store      *Store
	completion completionStore
	runner     *Runner
	slots      chan struct{}
}

func NewChatService(s *Store, r *Runner) *ChatService {
	return &ChatService{store: s, completion: s, runner: r, slots: make(chan struct{}, 2)}
}
func (s *ChatService) acquire() (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	default:
		return nil, NewError("AI_BUSY")
	}
}
func (s *ChatService) generate(ctx context.Context, a Actor, turn Turn, emit Emit) (RunResult, error) {
	result, err := s.runner.Run(ctx, a, turn, emit)
	status := "COMPLETED"
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		status = "INTERRUPTED"
	} else if err != nil || ctx.Err() != nil {
		status = "FAILED"
	}
	finish := func(c context.Context, state string) error {
		return s.completion.FinishTurn(c, a, turn.Assistant.ID, state, result.Content, result.Citations, result.Usage.InputTokens, result.Usage.OutputTokens)
	}
	if status == "COMPLETED" {
		if finishErr := finish(ctx, status); finishErr == nil {
			return result, nil
		} else {
			err = publicError(finishErr)
			status = "FAILED"
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if finishErr := finish(cleanup, status); finishErr != nil {
		slog.Error("AI message cleanup failed", "code", Code(publicError(finishErr)))
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = NewError("INTERNAL_ERROR")
	}
	return result, publicError(err)
}

package rag

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type rollbackProbe struct {
	pgx.Tx
	context context.Context
	bounded bool
}

func (p *rollbackProbe) Rollback(ctx context.Context) error {
	p.context = ctx
	deadline, ok := ctx.Deadline()
	p.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 3*time.Second && ctx.Err() == nil
	return nil
}
func TestRollbackUsesLiveBoundedIndependentContext(t *testing.T) {
	p := &rollbackProbe{}
	rollbackIndex(p)
	if !p.bounded || !errors.Is(p.context.Err(), context.Canceled) {
		t.Fatal("rollback needs a live deadline and must release its context afterward")
	}
}
func TestLocalDatabaseDeadlineMapsWithoutOuterDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := errors.New("driver detail")
	databaseResult(ctx, &err, "INDEX_TIMEOUT")
	if publicCode(err) != "INDEX_TIMEOUT" {
		t.Fatal("local SQL deadline leaked driver error")
	}
	var success error
	databaseResult(ctx, &success, "INDEX_TIMEOUT")
	if success != nil {
		t.Fatal("successful commit must not be changed into failure by later clock expiry")
	}
}

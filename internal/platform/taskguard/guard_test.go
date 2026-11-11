package taskguard

import (
	"context"
	"testing"
)

func TestCleanupContextPreservesImmutableGuard(t *testing.T) {
	g := Guard{JobID: 1, OrgID: 2, DocID: 3, RunVersion: 4, ActorID: 5, Token: "01234567890123456789012345678901"}
	ctx, cancel := context.WithCancel(With(context.Background(), g))
	cancel()
	clean := context.WithoutCancel(ctx)
	got, ok := From(clean)
	if !ok || got != g || clean.Err() != nil {
		t.Fatal("cleanup lost guard")
	}
	if Token(context.Background()) != nil {
		t.Fatal("sync ownership must be SQL NULL")
	}
}

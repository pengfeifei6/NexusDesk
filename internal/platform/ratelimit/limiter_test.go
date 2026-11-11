package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestInvalidScopeRejectedBeforeRedis(t *testing.T) {
	l := New(nil, 10, time.Minute, 100*time.Millisecond)
	for _, ids := range [][2]int64{{0, 1}, {1, 0}, {-1, 2}} {
		if ok, _, err := l.Check(context.Background(), ids[0], ids[1]); ok || err == nil {
			t.Fatal("invalid trusted scope accepted")
		}
	}
}

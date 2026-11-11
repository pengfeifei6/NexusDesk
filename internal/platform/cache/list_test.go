package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

type failedRevision struct{}
type failedRow struct{}

func (failedRevision) QueryRow(context.Context, string, ...any) pgx.Row { return failedRow{} }
func (failedRow) Scan(...any) error                                     { return errors.New("database unavailable") }

// Failure to fetch a generation must never put a successful list under zero.
func TestRevisionFailureFallsBackWithoutCaching(t *testing.T) {
	client := newClientForTest(t)
	c := New(client, failedRevision{}, Options{})
	data, e := List(context.Background(), c, 7, "tickets", struct{}{}, func() ([]int, error) { return []int{42}, nil }, func(int) bool { return true })
	if e != nil || len(data) != 1 || data[0] != 42 || c.Stats().Failures != 1 || c.Stats().Stores != 0 {
		t.Fatalf("fallback=%v %v stats=%+v", data, e, c.Stats())
	}
}
func TestLoadErrorIsPreservedAndNeverCached(t *testing.T) {
	want := errors.New("list query failed")
	data, e := List[int](context.Background(), nil, 7, "tickets", struct{}{}, func() ([]int, error) { return nil, want }, func(int) bool { return true })
	if data != nil || !errors.Is(e, want) {
		t.Fatal("database error hidden", data, e)
	}
}
func newClientForTest(t *testing.T) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { c.Close() })
	return c
}

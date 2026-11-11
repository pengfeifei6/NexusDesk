package taskqueue

import (
	"context"
	"errors"
	"github.com/hibiken/asynq"
	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"testing"
	"time"
)

func TestHandlerRejectsPayloadBeforeExecutionAndRedactsFailures(t *testing.T) {
	calls := 0
	h := handler(func(context.Context, jobs.Payload) error { calls++; return errors.New("private/path/password/content") })
	for _, raw := range []string{`{}`, `{"job_id":1,"org_id":2,"run_version":1,"text":"private"}`, `{"job_id":1,"job_id":2,"org_id":2,"run_version":1}`} {
		if err := h.ProcessTask(context.Background(), asynq.NewTask(TaskType, []byte(raw))); !errors.Is(err, asynq.SkipRetry) {
			t.Fatal("invalid payload retried")
		}
	}
	if calls != 0 {
		t.Fatal("malformed payload executed")
	}
	err := h.ProcessTask(context.Background(), asynq.NewTask(TaskType, []byte(`{"job_id":1,"org_id":2,"run_version":1}`)))
	if err == nil || err.Error() != "job delivery temporarily unavailable" || calls != 1 {
		t.Fatalf("unsafe or incorrect error %v", err)
	}
}

func TestTransportRetryAndTaskIdentity(t *testing.T) {
	if retryDelay(0, nil, nil) != 5*time.Second || retryDelay(1, nil, nil) != 30*time.Second {
		t.Fatal("transport backoff changed")
	}
	a := taskID(jobs.Payload{JobID: 12, OrgID: 3, RunVersion: 4}, 5)
	if a == taskID(jobs.Payload{JobID: 12, OrgID: 3, RunVersion: 5}, 5) || a == taskID(jobs.Payload{JobID: 12, OrgID: 3, RunVersion: 4}, 6) {
		t.Fatal("run or dispatch identity lost")
	}
}

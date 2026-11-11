package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"time"
)

type Payload struct {
	JobID      int64 `json:"job_id"`
	OrgID      int64 `json:"org_id"`
	RunVersion int64 `json:"run_version"`
}
type Enqueuer interface {
	Enqueue(context.Context, Payload, int64) error
}
type Limits struct {
	Enabled                                                         bool
	LeaseDuration, HeartbeatInterval, TaskTimeout, DispatchInterval time.Duration
}

func (l Limits) normalized() Limits {
	if l.LeaseDuration <= 0 {
		l.LeaseDuration = 30 * time.Second
	}
	if l.HeartbeatInterval <= 0 {
		l.HeartbeatInterval = 5 * time.Second
	}
	if l.TaskTimeout <= 0 {
		l.TaskTimeout = 180 * time.Second
	}
	if l.DispatchInterval <= 0 {
		l.DispatchInterval = 2 * time.Second
	}
	return l
}

type Job struct {
	ID                int64      `json:"id"`
	OrgID             int64      `json:"org_id"`
	DocumentID        int64      `json:"document_id"`
	RequestedBy       int64      `json:"requested_by"`
	ActorUserID       int64      `json:"actor_user_id"`
	Status            string     `json:"status"`
	Phase             string     `json:"phase"`
	RunVersion        int64      `json:"run_version"`
	Attempts          int        `json:"attempts"`
	MaxAttempts       int        `json:"max_attempts"`
	TotalAttempts     int64      `json:"total_attempts"`
	FailureCode       *string    `json:"failure_code"`
	Retryable         bool       `json:"retryable"`
	LastDispatchError *string    `json:"last_dispatch_error"`
	SourceSHA256      string     `json:"source_sha256"`
	EmbeddingModel    string     `json:"embedding_model"`
	ChunkerVersion    string     `json:"chunker_version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	StartedAt         *time.Time `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	NextRunAt         time.Time  `json:"next_run_at"`
}

func ParsePayload(data []byte) (Payload, error) {
	var p Payload
	if len(data) > 1024 {
		return p, newError("INVALID_REQUEST")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return p, newError("INVALID_REQUEST")
	}
	seen := map[string]bool{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return Payload{}, newError("INVALID_REQUEST")
		}
		key, ok := k.(string)
		if !ok || seen[key] {
			return Payload{}, newError("INVALID_REQUEST")
		}
		seen[key] = true
		v, e := d.Token()
		if e != nil {
			return Payload{}, newError("INVALID_REQUEST")
		}
		n, ok := v.(json.Number)
		if !ok {
			return Payload{}, newError("INVALID_REQUEST")
		}
		id, e := positive(string(n))
		if e != nil {
			return Payload{}, e
		}
		switch key {
		case "job_id":
			p.JobID = id
		case "org_id":
			p.OrgID = id
		case "run_version":
			p.RunVersion = id
		default:
			return Payload{}, newError("INVALID_REQUEST")
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(seen) != 3 {
		return Payload{}, newError("INVALID_REQUEST")
	}
	if _, e = d.Token(); e != io.EOF {
		return Payload{}, newError("INVALID_REQUEST")
	}
	return p, nil
}
func positive(raw string) (int64, error) {
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
		return 0, newError("INVALID_REQUEST")
	}
	return n, nil
}
func backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 5 * time.Second
	}
	return 30 * time.Second
}

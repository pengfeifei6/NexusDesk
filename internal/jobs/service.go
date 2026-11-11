package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskguard"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"time"
)

type documentService interface {
	Get(context.Context, int64, int64, int64) (knowledge.Document, error)
	Process(context.Context, int64, int64, int64) (knowledge.Document, error)
}
type indexService interface {
	Index(context.Context, int64, int64, int64) (rag.IndexInfo, error)
}
type Service struct {
	pool      *pgxpool.Pool
	documents documentService
	indexer   indexService
	limits    Limits
}

func NewService(pool *pgxpool.Pool, documents *knowledge.Service, indexer *rag.Service, limits Limits) *Service {
	return &Service{pool: pool, documents: documents, indexer: indexer, limits: limits.normalized()}
}
func newToken() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func (s *Service) claim(ctx context.Context, payload Payload) (claimed record, ok bool, err error) {
	if payload.JobID <= 0 || payload.OrgID <= 0 || payload.RunVersion <= 0 {
		return claimed, false, newError("INVALID_REQUEST")
	}
	err = s.withJob(ctx, payload.OrgID, payload.JobID, func(ctx context.Context, tx pgx.Tx, p parent, r record) error {
		if r.RunVersion != payload.RunVersion || (r.Status != "PENDING" && r.Status != "RETRYING") || !r.due {
			return nil
		}
		code, e := invalidSource(ctx, tx, p, r)
		if e != nil {
			return e
		}
		if code != "" {
			status := "FAILED"
			if code == "DOCUMENT_DELETED" {
				status = "CANCELED"
			}
			_, e = transition(ctx, tx, r, status, code, false)
			return e
		}
		complete, e := currentComplete(ctx, tx, r)
		if e != nil {
			return e
		}
		if complete {
			_, e = transition(ctx, tx, r, "SUCCEEDED", "", false)
			return e
		}
		if r.Attempts >= r.MaxAttempts {
			_, e = transition(ctx, tx, r, "FAILED", "LEASE_EXPIRED", false)
			return e
		}
		token, e := newToken()
		if e != nil {
			return e
		}
		claimed, e = scanRecord(tx.QueryRow(ctx, `UPDATE knowledge_jobs SET status='RUNNING',phase='EXTRACTING',attempts=attempts+1,total_attempts=total_attempts+1,lease_token=$3,lease_until=clock_timestamp()+$4*interval '1 second',started_at=COALESCE(started_at,clock_timestamp()),completed_at=NULL,failure_code=NULL,retryable=false,updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 RETURNING `+columns+`,lease_token,lease_until,dispatch_seq,true`, r.OrgID, r.ID, token, s.limits.LeaseDuration.Seconds()))
		ok = e == nil
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	return claimed, ok, nil
}
func (s *Service) renew(ctx context.Context, r record) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, e := s.pool.Exec(ctx, `UPDATE knowledge_jobs SET lease_until=clock_timestamp()+$5*interval '1 second',updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND run_version=$3 AND lease_token=$4 AND status='RUNNING' AND lease_until>clock_timestamp()`, r.OrgID, r.ID, r.RunVersion, *r.token, s.limits.LeaseDuration.Seconds())
	return e == nil && tag.RowsAffected() == 1, e
}
func (s *Service) phase(ctx context.Context, r record, phase string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, e := s.pool.Exec(ctx, `UPDATE knowledge_jobs SET phase=$5,updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND run_version=$3 AND lease_token=$4 AND status='RUNNING' AND lease_until>clock_timestamp()`, r.OrgID, r.ID, r.RunVersion, *r.token, phase)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return taskguard.ErrRevoked
	}
	return nil
}
func (s *Service) Execute(ctx context.Context, payload Payload) error {
	if ctx.Err() != nil {
		return newError("DELIVERY_RETRY")
	}
	// The operation budget includes claim; cleanup has its own short deadline.
	ctx, stopOperation := context.WithTimeout(ctx, s.limits.TaskTimeout)
	defer stopOperation()
	r, ok, e := s.claim(ctx, payload)
	if e != nil {
		if Code(e) == "INVALID_REQUEST" {
			return e
		}
		return newError("DELIVERY_RETRY")
	}
	if !ok {
		return nil
	}
	operation, cancel := context.WithTimeout(ctx, s.limits.TaskTimeout)
	defer cancel()
	operation = taskguard.With(operation, taskguard.Guard{JobID: r.ID, OrgID: r.OrgID, DocID: r.DocumentID, RunVersion: r.RunVersion, ActorID: r.ActorUserID, Token: *r.token})
	stopped := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(s.limits.HeartbeatInterval)
		defer tick.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-operation.Done():
				return
			case <-tick.C:
				valid, e := s.renew(operation, r)
				if e != nil || !valid {
					cancel()
					return
				}
			}
		}
	}()
	d, e := s.documents.Get(operation, r.ActorUserID, r.OrgID, r.DocumentID)
	if e == nil && d.Status != knowledge.Ready {
		_, e = s.documents.Process(operation, r.ActorUserID, r.OrgID, r.DocumentID)
	}
	if e == nil {
		e = s.phase(operation, r, "INDEXING")
	}
	if e == nil {
		_, e = s.indexer.Index(operation, r.ActorUserID, r.OrgID, r.DocumentID)
	}
	if e == nil {
		e = operation.Err()
	}
	close(stopped)
	<-done
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(operation), 5*time.Second)
	defer stop()
	if err := s.finish(finishCtx, r, e); err != nil {
		return newError("DELIVERY_RETRY")
	}
	return nil
}
func (s *Service) finish(ctx context.Context, attempt record, cause error) error {
	return s.withJob(ctx, attempt.OrgID, attempt.ID, func(ctx context.Context, tx pgx.Tx, p parent, r record) error {
		if r.Status != "RUNNING" || r.RunVersion != attempt.RunVersion || r.token == nil || *r.token != *attempt.token {
			return nil
		}
		// Expired attempts belong to recovery, never to a late worker publication.
		var live bool
		if e := tx.QueryRow(ctx, `SELECT lease_until>clock_timestamp() FROM knowledge_jobs WHERE org_id=$1 AND id=$2`, r.OrgID, r.ID).Scan(&live); e != nil {
			return e
		}
		if !live {
			return newError("DELIVERY_RETRY")
		}
		code, e := invalidSource(ctx, tx, p, r)
		if e != nil {
			return e
		}
		retryable := false
		status := "FAILED"
		if code == "DOCUMENT_DELETED" {
			status = "CANCELED"
		}
		if code == "" {
			complete, e := currentComplete(ctx, tx, r)
			if e != nil {
				return e
			}
			if complete {
				_, e = transition(ctx, tx, r, "SUCCEEDED", "", false)
				return e
			}
			if cause == nil {
				code, retryable = "STORAGE_ERROR", true
			} else {
				code, retryable = classify(cause)
			}
			if retryable && r.Attempts < r.MaxAttempts {
				status = "RETRYING"
			}
		}
		if e = cleanup(ctx, tx, r); e != nil {
			return e
		}
		_, e = transition(ctx, tx, r, status, code, retryable)
		return e
	})
}

// recoverExpired deliberately does not call taskguard.Check: expiry/revocation
// invalidates the ordinary guard, but the ended token still proves cleanup ownership.
func (s *Service) recoverExpired(ctx context.Context, org, id int64) error {
	return s.withJob(ctx, org, id, func(ctx context.Context, tx pgx.Tx, p parent, r record) error {
		if r.Status != "RUNNING" {
			return nil
		}
		var expired bool
		if e := tx.QueryRow(ctx, `SELECT lease_until<=clock_timestamp() FROM knowledge_jobs WHERE org_id=$1 AND id=$2`, org, id).Scan(&expired); e != nil {
			return e
		}
		if !expired {
			return nil
		}
		code, e := invalidSource(ctx, tx, p, r)
		if e != nil {
			return e
		}
		status := "FAILED"
		retryable := false
		if code == "DOCUMENT_DELETED" {
			status = "CANCELED"
		}
		if code == "" {
			complete, e := currentComplete(ctx, tx, r)
			if e != nil {
				return e
			}
			if complete {
				_, e = transition(ctx, tx, r, "SUCCEEDED", "", false)
				return e
			}
			code = "LEASE_EXPIRED"
			retryable = true
			if r.Attempts < r.MaxAttempts {
				status = "RETRYING"
			}
		}
		if e = cleanup(ctx, tx, r); e != nil {
			return e
		}
		_, e = transition(ctx, tx, r, status, code, retryable)
		return e
	})
}

type candidate struct{ org, id int64 }

func (s *Service) candidates(ctx context.Context, expired bool) ([]candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	predicate := `status IN ('PENDING','RETRYING') AND next_run_at<=clock_timestamp() AND next_dispatch_at<=clock_timestamp()`
	if expired {
		predicate = `status='RUNNING' AND lease_until<=clock_timestamp()`
	}
	rows, e := s.pool.Query(ctx, `SELECT org_id,id FROM knowledge_jobs WHERE `+predicate+` ORDER BY id LIMIT 50`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []candidate
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.org, &c.id); e != nil {
			return nil, e
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (s *Service) Dispatch(ctx context.Context, queue Enqueuer) error {
	expired, e := s.candidates(ctx, true)
	if e != nil {
		return newError("DELIVERY_RETRY")
	}
	failed := false
	for _, c := range expired {
		if e = s.recoverExpired(ctx, c.org, c.id); e != nil {
			failed = true
		}
	}
	due, e := s.candidates(ctx, false)
	if e != nil {
		return newError("DELIVERY_RETRY")
	}
	for _, c := range due {
		// This reservation locks only the job; it never subsequently takes a parent lock.
		reserve, stop := context.WithTimeout(ctx, 3*time.Second)
		var p Payload
		var seq int64
		e = s.pool.QueryRow(reserve, `UPDATE knowledge_jobs SET dispatch_seq=dispatch_seq+1,next_dispatch_at=clock_timestamp()+interval '15 seconds' WHERE org_id=$1 AND id=$2 AND status IN ('PENDING','RETRYING') AND next_run_at<=clock_timestamp() AND next_dispatch_at<=clock_timestamp() RETURNING id,org_id,run_version,dispatch_seq`, c.org, c.id).Scan(&p.JobID, &p.OrgID, &p.RunVersion, &seq)
		stop()
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			failed = true
			continue
		}
		delivery, stop := context.WithTimeout(ctx, 3*time.Second)
		sendErr := queue.Enqueue(delivery, p, seq)
		stop()
		var code any
		if sendErr != nil {
			code = "QUEUE_UNAVAILABLE"
			failed = true
		}
		outcome, stop := context.WithTimeout(ctx, 3*time.Second)
		_, e = s.pool.Exec(outcome, `UPDATE knowledge_jobs SET last_dispatch_error=$5,updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND run_version=$3 AND dispatch_seq=$4`, p.OrgID, p.JobID, p.RunVersion, seq, code)
		stop()
		if e != nil {
			failed = true
		}
	}
	if failed {
		return newError("DELIVERY_RETRY")
	}
	return nil
}

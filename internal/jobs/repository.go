package jobs

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

const columns = `id,org_id,document_id,requested_by,actor_user_id,status,phase,run_version,attempts,max_attempts,total_attempts,failure_code,retryable,last_dispatch_error,source_sha256,embedding_model,chunker_version,created_at,updated_at,started_at,completed_at,next_run_at`

type record struct {
	Job
	token       *string
	until       *time.Time
	dispatchSeq int64
	due         bool
}

func scan(row pgx.Row) (Job, error) {
	var j Job
	e := row.Scan(&j.ID, &j.OrgID, &j.DocumentID, &j.RequestedBy, &j.ActorUserID, &j.Status, &j.Phase, &j.RunVersion, &j.Attempts, &j.MaxAttempts, &j.TotalAttempts, &j.FailureCode, &j.Retryable, &j.LastDispatchError, &j.SourceSHA256, &j.EmbeddingModel, &j.ChunkerVersion, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt, &j.NextRunAt)
	return j, e
}
func scanRecord(row pgx.Row) (record, error) {
	var r record
	j := &r.Job
	e := row.Scan(&j.ID, &j.OrgID, &j.DocumentID, &j.RequestedBy, &j.ActorUserID, &j.Status, &j.Phase, &j.RunVersion, &j.Attempts, &j.MaxAttempts, &j.TotalAttempts, &j.FailureCode, &j.Retryable, &j.LastDispatchError, &j.SourceSHA256, &j.EmbeddingModel, &j.ChunkerVersion, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.CompletedAt, &j.NextRunAt, &r.token, &r.until, &r.dispatchSeq, &r.due)
	return r, e
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func dbError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return newError("NOT_FOUND")
	}
	var safe *Error
	if errors.As(e, &safe) {
		return e
	}
	return newError("INTERNAL_ERROR")
}

type parent struct {
	hash, status string
	deleted      bool
}

func lockParent(ctx context.Context, tx pgx.Tx, org, id int64) (parent, error) {
	var p parent
	e := tx.QueryRow(ctx, `SELECT sha256_hex,status,deleted_at IS NOT NULL FROM knowledge_documents WHERE org_id=$1 AND id=$2 FOR UPDATE`, org, id).Scan(&p.hash, &p.status, &p.deleted)
	return p, e
}
func lockJob(ctx context.Context, tx pgx.Tx, org, id int64) (record, error) {
	return scanRecord(tx.QueryRow(ctx, `SELECT `+columns+`,lease_token,lease_until,dispatch_seq,next_run_at<=clock_timestamp() FROM knowledge_jobs WHERE org_id=$1 AND id=$2 FOR UPDATE`, org, id))
}
func membership(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor, org int64) (string, error) {
	if actor <= 0 {
		return "", newError("UNAUTHENTICATED")
	}
	if org <= 0 {
		return "", newError("INVALID_REQUEST")
	}
	var role string
	e := q.QueryRow(ctx, `SELECT role FROM organization_members WHERE org_id=$1 AND user_id=$2 AND status='active'`, org, actor).Scan(&role)
	return role, dbError(e)
}
func (s *Service) authorize(ctx context.Context, actor, org int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, e := membership(ctx, s.pool, actor, org)
	return e
}

// withJob discovers the parent without locking, then always locks parent before job.
func (s *Service) withJob(ctx context.Context, org, id int64, fn func(context.Context, pgx.Tx, parent, record) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollback(tx)
	var doc int64
	if e = tx.QueryRow(ctx, `SELECT document_id FROM knowledge_jobs WHERE org_id=$1 AND id=$2`, org, id).Scan(&doc); e != nil {
		return e
	}
	p, e := lockParent(ctx, tx, org, doc)
	if e != nil {
		return e
	}
	r, e := lockJob(ctx, tx, org, id)
	if e != nil {
		return e
	}
	if e = fn(ctx, tx, p, r); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) Submit(ctx context.Context, actor, org, doc int64) (out Job, resultErr error) {
	defer func() { resultErr = dbError(resultErr) }()
	if e := s.authorize(ctx, actor, org); e != nil {
		return out, e
	}
	if doc <= 0 {
		return out, newError("INVALID_REQUEST")
	}
	if !s.limits.Enabled {
		return out, newError("ASYNC_DISABLED")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer rollback(tx)
	p, e := lockParent(ctx, tx, org, doc)
	if e != nil {
		return out, e
	}
	if p.deleted {
		return out, newError("NOT_FOUND")
	}
	if _, e = membership(ctx, tx, actor, org); e != nil {
		return out, e
	}
	out, e = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM knowledge_jobs WHERE org_id=$1 AND document_id=$2 AND source_sha256=$3 AND extractor_version='utf8-v1' AND embedding_model='bge-m3:567m' AND chunker_version='rune-500-80-v1'`, org, doc, p.hash))
	if e == nil {
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var indexing bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 AND status='INDEXING')`, org, doc).Scan(&indexing); e != nil {
		return out, e
	}
	if p.status == "PROCESSING" || indexing {
		return out, newError("DOCUMENT_STATE_CONFLICT")
	}
	out, e = scan(tx.QueryRow(ctx, `INSERT INTO knowledge_jobs(org_id,document_id,requested_by,actor_user_id,source_sha256) VALUES($1,$2,$3,$3,$4) RETURNING `+columns, org, doc, actor, p.hash))
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) List(ctx context.Context, actor, org, doc int64) ([]Job, error) {
	if e := s.authorize(ctx, actor, org); e != nil {
		return nil, e
	}
	if doc <= 0 {
		return nil, newError("INVALID_REQUEST")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var visible bool
	e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL)`, org, doc).Scan(&visible)
	if e != nil {
		return nil, dbError(e)
	}
	if !visible {
		return nil, newError("NOT_FOUND")
	}
	rows, e := s.pool.Query(ctx, `SELECT `+columns+` FROM knowledge_jobs WHERE org_id=$1 AND document_id=$2 ORDER BY id DESC LIMIT 20`, org, doc)
	if e != nil {
		return nil, dbError(e)
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, e := scan(rows)
		if e != nil {
			return nil, dbError(e)
		}
		out = append(out, j)
	}
	return out, dbError(rows.Err())
}
func (s *Service) Get(ctx context.Context, actor, org, id int64) (Job, error) {
	if e := s.authorize(ctx, actor, org); e != nil {
		return Job{}, e
	}
	if id <= 0 {
		return Job{}, newError("INVALID_REQUEST")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	j, e := scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM knowledge_jobs j WHERE j.org_id=$1 AND j.id=$2 AND EXISTS(SELECT 1 FROM knowledge_documents d WHERE d.org_id=j.org_id AND d.id=j.document_id AND d.deleted_at IS NULL)`, org, id))
	return j, dbError(e)
}
func (s *Service) Retry(ctx context.Context, actor, org, id int64) (Job, error) {
	return s.mutate(ctx, actor, org, id, true)
}
func (s *Service) Cancel(ctx context.Context, actor, org, id int64) (Job, error) {
	return s.mutate(ctx, actor, org, id, false)
}
func (s *Service) mutate(ctx context.Context, actor, org, id int64, retry bool) (out Job, err error) {
	if e := s.authorize(ctx, actor, org); e != nil {
		return out, e
	}
	if id <= 0 {
		return out, newError("INVALID_REQUEST")
	}
	if retry && !s.limits.Enabled {
		return out, newError("ASYNC_DISABLED")
	}
	err = s.withJob(ctx, org, id, func(ctx context.Context, tx pgx.Tx, p parent, r record) error {
		if p.deleted {
			return newError("NOT_FOUND")
		}
		role, e := membership(ctx, tx, actor, org)
		if e != nil {
			return e
		}
		if actor != r.RequestedBy && role != "admin" {
			return newError("FORBIDDEN")
		}
		if retry {
			if r.Status != "FAILED" && r.Status != "CANCELED" {
				return newError("JOB_STATE_CONFLICT")
			}
			if p.hash != r.SourceSHA256 || p.status == "PROCESSING" {
				return newError("DOCUMENT_STATE_CONFLICT")
			}
			var indexing bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 AND status='INDEXING')`, org, r.DocumentID).Scan(&indexing); e != nil {
				return e
			}
			if indexing {
				return newError("DOCUMENT_STATE_CONFLICT")
			}
			out, e = scan(tx.QueryRow(ctx, `UPDATE knowledge_jobs SET status='PENDING',phase='WAITING',actor_user_id=$3,run_version=run_version+1,attempts=0,failure_code=NULL,retryable=false,started_at=NULL,completed_at=NULL,next_run_at=clock_timestamp(),next_dispatch_at=clock_timestamp(),last_dispatch_error=NULL,updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 RETURNING `+columns, org, id, actor))
			return e
		}
		if r.Status != "PENDING" && r.Status != "RUNNING" && r.Status != "RETRYING" {
			return newError("JOB_STATE_CONFLICT")
		}
		if e = cleanup(ctx, tx, r); e != nil {
			return e
		}
		out, e = transition(ctx, tx, r, "CANCELED", "PROCESS_CANCELED", false)
		return e
	})
	return out, dbError(err)
}

// cleanup is only for ended attempts, under parent and job locks. It never uses
// the live guard and never resets legacy unowned rows or completed publications.
func cleanup(ctx context.Context, tx pgx.Tx, r record) error {
	if r.token == nil {
		return nil
	}
	if _, e := tx.Exec(ctx, `UPDATE knowledge_documents SET status='FAILED',failure_code='PROCESS_CANCELED',extracted_chars=0,task_token=NULL,updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND status='PROCESSING' AND task_token=$3`, r.OrgID, r.DocumentID, *r.token); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `UPDATE knowledge_document_indexes SET status='FAILED',failure_code='PROCESS_CANCELED',chunk_count=0,indexed_at=NULL,task_token=NULL,updated_at=clock_timestamp() WHERE org_id=$1 AND document_id=$2 AND status='INDEXING' AND task_token=$3`, r.OrgID, r.DocumentID, *r.token)
	return e
}
func transition(ctx context.Context, tx pgx.Tx, r record, status, code string, retryable bool) (Job, error) {
	var failure any
	if code != "" {
		failure = code
	}
	delay := time.Duration(0)
	if status == "RETRYING" {
		delay = backoff(r.Attempts)
	}
	return scan(tx.QueryRow(ctx, `UPDATE knowledge_jobs SET status=$3,phase=CASE WHEN $3='SUCCEEDED' THEN 'DONE' ELSE phase END,failure_code=$4,retryable=$5,lease_token=NULL,lease_until=NULL,completed_at=CASE WHEN $3 IN ('FAILED','CANCELED','SUCCEEDED') THEN clock_timestamp() ELSE NULL END,next_run_at=clock_timestamp()+$6*interval '1 second',next_dispatch_at=clock_timestamp()+$6*interval '1 second',updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 RETURNING `+columns, r.OrgID, r.ID, status, failure, retryable, delay.Seconds()))
}
func currentComplete(ctx context.Context, tx pgx.Tx, r record) (bool, error) {
	var complete bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_documents d JOIN knowledge_document_texts t ON t.org_id=d.org_id AND t.document_id=d.id JOIN knowledge_document_indexes i ON i.org_id=d.org_id AND i.document_id=d.id WHERE d.org_id=$1 AND d.id=$2 AND d.deleted_at IS NULL AND d.status='READY' AND d.sha256_hex=$3 AND t.extractor_version='utf8-v1' AND i.status='READY' AND i.source_content_sha256=t.content_sha256 AND i.embedding_model=$4 AND i.embedding_dimension=1024 AND i.chunker_version=$5 AND i.chunk_count>0 AND i.chunk_count=(SELECT count(*) FROM knowledge_chunks c WHERE c.org_id=d.org_id AND c.document_id=d.id AND c.content_sha256=t.content_sha256 AND c.index_version=i.index_version AND c.embedding_model=i.embedding_model AND c.chunker_version=i.chunker_version))`, r.OrgID, r.DocumentID, r.SourceSHA256, r.EmbeddingModel, r.ChunkerVersion).Scan(&complete)
	return complete, e
}
func invalidSource(ctx context.Context, tx pgx.Tx, p parent, r record) (string, error) {
	if p.deleted {
		return "DOCUMENT_DELETED", nil
	}
	if p.hash != r.SourceSHA256 {
		return "SOURCE_CHANGED", nil
	}
	_, e := membership(ctx, tx, r.ActorUserID, r.OrgID)
	if Code(e) == "NOT_FOUND" {
		return "AUTHORIZATION_REVOKED", nil
	}
	return "", e
}

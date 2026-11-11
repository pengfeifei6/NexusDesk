package knowledge

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskguard"
	"time"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository { return &PostgresRepository{pool} }

const documentColumns = "id,org_id,uploaded_by,original_filename,storage_key,media_type,size_bytes,sha256_hex,status,failure_code,extracted_chars,created_at,updated_at"

func scanDocument(row pgx.Row) (Document, error) {
	var d Document
	err := row.Scan(&d.ID, &d.OrgID, &d.UploadedBy, &d.OriginalFilename, &d.StorageKey, &d.MediaType, &d.SizeBytes, &d.SHA256Hex, &d.Status, &d.FailureCode, &d.ExtractedChars, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, ErrStorage
	}
	return d, nil
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func commit(tx pgx.Tx, ctx context.Context) error {
	err := tx.Commit(ctx)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.Is(err, pgx.ErrTxCommitRollback) || errors.As(err, &pgErr) {
		return ErrStorage
	}
	return ErrUncertain
}
func (r *PostgresRepository) Create(ctx context.Context, d Document) (Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Document{}, ErrStorage
	}
	defer rollback(tx)
	result, err := scanDocument(tx.QueryRow(ctx, `INSERT INTO knowledge_documents(org_id,uploaded_by,original_filename,storage_key,media_type,size_bytes,sha256_hex) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+documentColumns, d.OrgID, d.UploadedBy, d.OriginalFilename, d.StorageKey, d.MediaType, d.SizeBytes, d.SHA256Hex))
	if err != nil {
		return Document{}, ErrStorage
	}
	if err = commit(tx, ctx); err != nil {
		return Document{}, err
	}
	return result, nil
}
func (r *PostgresRepository) Get(ctx context.Context, org, id int64) (Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return scanDocument(r.pool.QueryRow(ctx, `SELECT `+documentColumns+` FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL`, org, id))
}
func (r *PostgresRepository) List(ctx context.Context, org int64, f Filter) ([]Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT `+documentColumns+` FROM knowledge_documents WHERE org_id=$1 AND deleted_at IS NULL AND ($2='' OR status=$2) ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, org, string(f.Status), f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, ErrStorage
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if rows.Err() != nil {
		return nil, ErrStorage
	}
	return out, nil
}

// processGuard requires the already-held parent lock. Legacy schemas never
// reference task_token; schema 11 synchronous ownership is explicitly NULL.
func processGuard(ctx context.Context, tx pgx.Tx, org, id int64, claim bool) (bool, error) {
	latest, e := taskguard.Check(ctx, tx, org, id, claim)
	if errors.Is(e, taskguard.ErrConflict) {
		return latest, ErrConflict
	}
	return latest, e
}
func (r *PostgresRepository) ClaimProcess(ctx context.Context, org, id int64) (Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return Document{}, ErrStorage
	}
	defer rollback(tx)
	current, e := scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id))
	if e != nil {
		return Document{}, e
	}
	latest, e := processGuard(ctx, tx, org, id, true)
	if e != nil {
		return Document{}, e
	}
	if current.Status != Uploaded && current.Status != Failed {
		return Document{}, ErrConflict
	}
	sql := `UPDATE knowledge_documents SET status='PROCESSING',failure_code=NULL,extracted_chars=0,updated_at=clock_timestamp()`
	args := []any{org, id}
	if latest {
		sql += `,task_token=$3`
		args = append(args, taskguard.Token(ctx))
	}
	sql += ` WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL AND status IN ('UPLOADED','FAILED') RETURNING ` + documentColumns
	d, e := scanDocument(tx.QueryRow(ctx, sql, args...))
	if e != nil {
		return Document{}, e
	}
	if e = commit(tx, ctx); e != nil {
		return Document{}, e
	}
	return d, nil
}
func (r *PostgresRepository) CompleteProcess(ctx context.Context, d Document, x ExtractedText) (Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return Document{}, ErrStorage
	}
	defer rollback(tx)
	current, e := scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, d.OrgID, d.ID))
	if e != nil {
		return Document{}, e
	}
	latest, e := processGuard(ctx, tx, d.OrgID, d.ID, false)
	if e != nil {
		return Document{}, e
	}
	if current.Status != Processing {
		return Document{}, ErrConflict
	}
	if current.SHA256Hex != d.SHA256Hex {
		return Document{}, ErrSourceChanged
	}
	if latest {
		var owned bool
		e = tx.QueryRow(ctx, `SELECT task_token IS NOT DISTINCT FROM $3::text FROM knowledge_documents WHERE org_id=$1 AND id=$2`, d.OrgID, d.ID, taskguard.Token(ctx)).Scan(&owned)
		if e != nil {
			return Document{}, ErrStorage
		}
		if !owned {
			return Document{}, ErrConflict
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO knowledge_document_texts(org_id,document_id,content_text,content_sha256,extractor_version) VALUES($1,$2,$3,$4,$5) ON CONFLICT(document_id) DO UPDATE SET content_text=EXCLUDED.content_text,content_sha256=EXCLUDED.content_sha256,extractor_version=EXCLUDED.extractor_version,extracted_at=clock_timestamp() WHERE knowledge_document_texts.org_id=EXCLUDED.org_id`, d.OrgID, d.ID, x.ContentText, x.ContentSHA256, x.ExtractorVersion)
	if e != nil {
		return Document{}, ErrStorage
	}
	if _, e = processGuard(ctx, tx, d.OrgID, d.ID, false); e != nil {
		return Document{}, e
	}
	sql := `UPDATE knowledge_documents SET status='READY',failure_code=NULL,extracted_chars=$3,updated_at=clock_timestamp()`
	args := []any{d.OrgID, d.ID, x.Chars}
	if latest {
		sql += `,task_token=NULL`
	}
	sql += ` WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL AND status='PROCESSING'`
	if latest {
		sql += ` AND task_token IS NOT DISTINCT FROM $4::text`
		args = append(args, taskguard.Token(ctx))
	}
	sql += ` RETURNING ` + documentColumns
	result, e := scanDocument(tx.QueryRow(ctx, sql, args...))
	if e != nil {
		return Document{}, e
	}
	if e = commit(tx, ctx); e != nil {
		return Document{}, e
	}
	return result, nil
}
func (r *PostgresRepository) FailProcess(ctx context.Context, org, id int64, code string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return ErrStorage
	}
	defer rollback(tx)
	_, e = scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id))
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	latest, e := processGuard(ctx, tx, org, id, false)
	if e != nil {
		return e
	}
	sql := `UPDATE knowledge_documents SET status='FAILED',failure_code=$3,extracted_chars=0,updated_at=clock_timestamp()`
	args := []any{org, id, code}
	if latest {
		sql += `,task_token=NULL`
	}
	sql += ` WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL AND status='PROCESSING'`
	if latest {
		sql += ` AND task_token IS NOT DISTINCT FROM $4::text`
		args = append(args, taskguard.Token(ctx))
	}
	if _, e = tx.Exec(ctx, sql, args...); e != nil {
		return ErrStorage
	}
	return commit(tx, ctx)
}
func (r *PostgresRepository) Delete(ctx context.Context, org, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `UPDATE knowledge_documents SET deleted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL`, org, id)
	if err != nil {
		return ErrStorage
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}
func (r *PostgresRepository) GetText(ctx context.Context, org, id int64) (DocumentText, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var x DocumentText
	err := r.pool.QueryRow(ctx, `SELECT t.document_id,t.content_text,t.content_sha256,t.extractor_version,t.extracted_at FROM knowledge_document_texts t JOIN knowledge_documents d ON d.org_id=t.org_id AND d.id=t.document_id WHERE d.org_id=$1 AND d.id=$2 AND d.deleted_at IS NULL AND d.status='READY'`, org, id).Scan(&x.DocumentID, &x.ContentText, &x.ContentSHA256, &x.ExtractorVersion, &x.ExtractedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		d, e := r.Get(ctx, org, id)
		if e != nil {
			return DocumentText{}, e
		}
		if d.Status != Ready {
			return DocumentText{}, ErrConflict
		}
		return DocumentText{}, ErrStorage
	}
	if err != nil {
		return DocumentText{}, ErrStorage
	}
	return x, nil
}

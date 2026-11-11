package rag

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskguard"
)

type indexRepository struct{ pool *pgxpool.Pool }

func rollbackIndex(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func databaseResult(ctx context.Context, err *error, code string) {
	if *err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		*err = NewError(code)
	}
}

const indexColumns = `document_id,status,index_version,embedding_model,embedding_dimension,chunker_version,source_content_sha256,chunk_count,failure_code,started_at,indexed_at,updated_at`

func scanIndex(row pgx.Row) (IndexInfo, error) {
	var i IndexInfo
	err := row.Scan(&i.DocumentID, &i.Status, &i.IndexVersion, &i.EmbeddingModel, &i.EmbeddingDimension, &i.ChunkerVersion, &i.SourceContentSHA256, &i.ChunkCount, &i.FailureCode, &i.StartedAt, &i.IndexedAt, &i.UpdatedAt)
	return i, err
}

// Parent, job guard, text, then index is the lock order for claim/publication.
func lockSource(ctx context.Context, tx pgx.Tx, org, id int64, claim bool) (content, hash string, latest bool, err error) {
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, knowledge.ErrNotFound
	}
	if err != nil {
		return "", "", false, err
	}
	latest, err = taskguard.Check(ctx, tx, org, id, claim)
	if errors.Is(err, taskguard.ErrConflict) {
		err = NewError("INDEX_STATE_CONFLICT")
	}
	if err != nil {
		return "", "", latest, err
	}
	if status != "READY" {
		return "", "", false, NewError("INDEX_STATE_CONFLICT")
	}
	err = tx.QueryRow(ctx, `SELECT content_text,content_sha256 FROM knowledge_document_texts WHERE org_id=$1 AND document_id=$2 FOR UPDATE`, org, id).Scan(&content, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		err = NewError("INDEX_STATE_CONFLICT")
	}
	return
}
func (r *indexRepository) Claim(ctx context.Context, org, id int64) (result indexClaim, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "INDEX_TIMEOUT")
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return indexClaim{}, err
	}
	defer rollbackIndex(tx)
	content, hash, latest, err := lockSource(ctx, tx, org, id, true)
	if err != nil {
		return indexClaim{}, err
	}
	old, err := scanIndex(tx.QueryRow(ctx, `SELECT `+indexColumns+` FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 FOR UPDATE`, org, id))
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return indexClaim{}, err
	}
	if exists && old.Status == "INDEXING" {
		return indexClaim{}, NewError("INDEX_STATE_CONFLICT")
	}
	if exists && old.Status == "READY" && old.SourceContentSHA256 == hash && old.EmbeddingModel == EmbeddingModel && old.EmbeddingDimension == EmbeddingDimension && old.ChunkerVersion == knowledge.ChunkerVersion {
		if err = tx.Commit(ctx); err != nil {
			return indexClaim{}, err
		}
		return indexClaim{Info: old, Content: content, Reused: true}, nil
	}
	var info IndexInfo
	if !exists {
		info, err = scanIndex(tx.QueryRow(ctx, `INSERT INTO knowledge_document_indexes(document_id,org_id,status,index_version,embedding_model,embedding_dimension,chunker_version,source_content_sha256) VALUES($1,$2,'INDEXING',1,$3,$4,$5,$6) RETURNING `+indexColumns, id, org, EmbeddingModel, EmbeddingDimension, knowledge.ChunkerVersion, hash))
	} else {
		info, err = scanIndex(tx.QueryRow(ctx, `UPDATE knowledge_document_indexes SET status='INDEXING',index_version=index_version+1,embedding_model=$3,embedding_dimension=$4,chunker_version=$5,source_content_sha256=$6,chunk_count=0,failure_code=NULL,started_at=clock_timestamp(),indexed_at=NULL,updated_at=clock_timestamp() WHERE org_id=$1 AND document_id=$2 RETURNING `+indexColumns, org, id, EmbeddingModel, EmbeddingDimension, knowledge.ChunkerVersion, hash))
	}
	if err != nil {
		return indexClaim{}, err
	}
	if latest {
		if _, err = tx.Exec(ctx, `UPDATE knowledge_document_indexes SET task_token=$3 WHERE org_id=$1 AND document_id=$2`, org, id, taskguard.Token(ctx)); err != nil {
			return indexClaim{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return indexClaim{}, err
	}
	return indexClaim{Info: info, Content: content}, nil
}
func (r *indexRepository) Finish(ctx context.Context, org int64, claim indexClaim, chunks []knowledge.Chunk, vectors [][]float32) (result IndexInfo, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "INDEX_TIMEOUT")
	if err := normalizeVectors(vectors, len(chunks)); err != nil {
		return IndexInfo{}, err
	}
	if len(chunks) == 0 || len(chunks) > knowledge.MaxIndexChunks {
		return IndexInfo{}, NewError("INDEX_CONTENT_INVALID")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return IndexInfo{}, err
	}
	defer rollbackIndex(tx)
	_, hash, latest, err := lockSource(ctx, tx, org, claim.Info.DocumentID, false)
	if errors.Is(err, knowledge.ErrNotFound) || publicCode(err) == "INDEX_STATE_CONFLICT" {
		return IndexInfo{}, NewError("INDEX_SOURCE_CHANGED")
	}
	if err != nil {
		return IndexInfo{}, err
	}
	current, err := scanIndex(tx.QueryRow(ctx, `SELECT `+indexColumns+` FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2 FOR UPDATE`, org, claim.Info.DocumentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return IndexInfo{}, NewError("INDEX_SOURCE_CHANGED")
	}
	if err != nil {
		return IndexInfo{}, err
	}
	if hash != claim.Info.SourceContentSHA256 || current.SourceContentSHA256 != hash || current.Status != "INDEXING" || current.IndexVersion != claim.Info.IndexVersion {
		return IndexInfo{}, NewError("INDEX_SOURCE_CHANGED")
	}
	if latest {
		var owned bool
		err = tx.QueryRow(ctx, `SELECT task_token IS NOT DISTINCT FROM $3::text FROM knowledge_document_indexes WHERE org_id=$1 AND document_id=$2`, org, claim.Info.DocumentID, taskguard.Token(ctx)).Scan(&owned)
		if err != nil {
			return IndexInfo{}, err
		}
		if !owned {
			return IndexInfo{}, NewError("INDEX_SOURCE_CHANGED")
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM knowledge_chunks WHERE org_id=$1 AND document_id=$2`, org, claim.Info.DocumentID); err != nil {
		return IndexInfo{}, err
	}
	for i, c := range chunks {
		_, err = tx.Exec(ctx, `INSERT INTO knowledge_chunks(org_id,document_id,chunk_index,content_text,start_rune,end_rune,content_sha256,embedding,embedding_model,chunker_version,index_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector,$9,$10,$11)`, org, claim.Info.DocumentID, c.Index, c.Content, c.StartRune, c.EndRune, hash, vectorLiteral(vectors[i]), EmbeddingModel, knowledge.ChunkerVersion, claim.Info.IndexVersion)
		if err != nil {
			return IndexInfo{}, err
		}
	}
	if _, err = taskguard.Check(ctx, tx, org, claim.Info.DocumentID, false); err != nil {
		return IndexInfo{}, err
	}
	sql := `UPDATE knowledge_document_indexes SET status='READY',chunk_count=$3,failure_code=NULL,indexed_at=clock_timestamp(),updated_at=clock_timestamp()`
	args := []any{org, claim.Info.DocumentID, len(chunks)}
	if latest {
		sql += `,task_token=NULL`
	}
	sql += ` WHERE org_id=$1 AND document_id=$2`
	if latest {
		sql += ` AND task_token IS NOT DISTINCT FROM $4::text`
		args = append(args, taskguard.Token(ctx))
	}
	sql += ` RETURNING ` + indexColumns
	info, err := scanIndex(tx.QueryRow(ctx, sql, args...))
	if err != nil {
		return IndexInfo{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return IndexInfo{}, err
	}
	return info, nil
}
func (r *indexRepository) Fail(ctx context.Context, org, id, version int64, code string) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "INDEX_TIMEOUT")
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackIndex(tx)
	var visible int64
	e = tx.QueryRow(ctx, `SELECT id FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id).Scan(&visible)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	latest, e := taskguard.Check(ctx, tx, org, id, false)
	if e != nil {
		return e
	}
	sql := `UPDATE knowledge_document_indexes SET status='FAILED',chunk_count=0,indexed_at=NULL,failure_code=$4,updated_at=clock_timestamp()`
	args := []any{org, id, version, code}
	if latest {
		sql += `,task_token=NULL`
	}
	sql += ` WHERE org_id=$1 AND document_id=$2 AND index_version=$3 AND status='INDEXING'`
	if latest {
		sql += ` AND task_token IS NOT DISTINCT FROM $5::text`
		args = append(args, taskguard.Token(ctx))
	}
	if _, e = tx.Exec(ctx, sql, args...); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *indexRepository) Status(ctx context.Context, org, id int64) (result IndexInfo, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "INDEX_TIMEOUT")
	info, err := scanIndex(r.pool.QueryRow(ctx, `SELECT i.document_id,i.status,i.index_version,i.embedding_model,i.embedding_dimension,i.chunker_version,i.source_content_sha256,i.chunk_count,i.failure_code,i.started_at,i.indexed_at,i.updated_at FROM knowledge_document_indexes i JOIN knowledge_documents d ON d.id=i.document_id AND d.org_id=i.org_id WHERE i.org_id=$1 AND i.document_id=$2 AND d.deleted_at IS NULL`, org, id))
	if !errors.Is(err, pgx.ErrNoRows) {
		return info, err
	}
	var visible bool
	err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL)`, org, id).Scan(&visible)
	if err != nil {
		return IndexInfo{}, err
	}
	if !visible {
		return IndexInfo{}, knowledge.ErrNotFound
	}
	return absentIndex(id), nil
}

// Chunks retain the full source text hash in content_sha256 (not a hash of
// their excerpt). Every query and final source check uses the same current-source
// predicates so a new extraction hides every chunk of the previous snapshot.
const currentSourceJoins = `
 JOIN knowledge_documents d ON d.org_id=c.org_id AND d.id=c.document_id
 JOIN knowledge_document_texts t ON t.org_id=c.org_id AND t.document_id=c.document_id
 JOIN knowledge_document_indexes i ON i.org_id=c.org_id AND i.document_id=c.document_id
`
const currentSourceWhere = ` c.org_id=$1 AND d.deleted_at IS NULL AND d.status='READY' AND i.status='READY'
 AND i.source_content_sha256=t.content_sha256 AND c.content_sha256=t.content_sha256
 AND c.index_version=i.index_version AND c.embedding_model=i.embedding_model
 AND i.embedding_model='bge-m3:567m' AND i.embedding_dimension=1024
 AND c.chunker_version=i.chunker_version AND i.chunker_version='rune-500-80-v1' `

func (r *indexRepository) Search(ctx context.Context, org int64, vector []float32, k int, minimum float64) (resultRows []sourceSnapshot, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "MODEL_TIMEOUT")
	if err := normalizeVectors([][]float32{vector}, 1); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.document_id,c.chunk_index,d.original_filename,c.content_text,c.start_rune,c.end_rune,c.embedding <=> $2::vector AS distance,t.content_sha256,i.index_version FROM knowledge_chunks c `+currentSourceJoins+` WHERE `+currentSourceWhere+` AND 1-(c.embedding <=> $2::vector)>=$3 ORDER BY distance ASC,c.id ASC LIMIT $4`, org, vectorLiteral(vector), minimum, k)
	if err != nil {
		return nil, err
	}
	return decodeSourceRows(rows, k)
}
func (r *indexRepository) Validate(ctx context.Context, org int64, sources []sourceSnapshot) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "MODEL_TIMEOUT")
	if len(sources) == 0 {
		return ctx.Err()
	}
	ids := make([]int64, len(sources))
	docs := make([]int64, len(sources))
	versions := make([]int64, len(sources))
	hashes := make([]string, len(sources))
	for n, s := range sources {
		ids[n] = s.ChunkID
		docs[n] = s.DocumentID
		versions[n] = s.Version
		hashes[n] = s.Hash
	}
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM unnest($2::bigint[],$3::bigint[],$4::bigint[],$5::text[]) AS expected(chunk_id,document_id,index_version,content_hash) JOIN knowledge_chunks c ON c.id=expected.chunk_id AND c.document_id=expected.document_id `+currentSourceJoins+` WHERE `+currentSourceWhere+` AND i.index_version=expected.index_version AND t.content_sha256=expected.content_hash`, org, ids, docs, versions, hashes).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(sources) {
		return NewError("INDEX_SOURCE_CHANGED")
	}
	return nil
}

// LexicalSearch ranks by the number of distinct literal query terms present.
// It deliberately shares every current-source and cosine predicate with Search.
func (r *indexRepository) LexicalSearch(ctx context.Context, org int64, vector []float32, terms []string, k int, minimum float64) (resultRows []sourceSnapshot, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer databaseResult(ctx, &resultErr, "MODEL_TIMEOUT")
	if len(terms) == 0 {
		return []sourceSnapshot{}, ctx.Err()
	}
	if err := normalizeVectors([][]float32{vector}, 1); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.document_id,c.chunk_index,d.original_filename,c.content_text,c.start_rune,c.end_rune,c.embedding <=> $2::vector AS distance,t.content_sha256,i.index_version FROM knowledge_chunks c `+currentSourceJoins+`
 CROSS JOIN LATERAL (SELECT count(*) AS hits FROM unnest($5::text[]) AS terms(term) WHERE strpos(lower(c.content_text),term)>0) lexical
 WHERE `+currentSourceWhere+` AND 1-(c.embedding <=> $2::vector)>=$3 AND lexical.hits>0 ORDER BY lexical.hits DESC,c.id ASC LIMIT $4`, org, vectorLiteral(vector), minimum, k, terms)
	if err != nil {
		return nil, err
	}
	return decodeSourceRows(rows, k)
}

// decodeSourceRows preserves the shared source projection and cosine validation
// for both candidate routes, including row closure on every return path.
func decodeSourceRows(rows pgx.Rows, k int) ([]sourceSnapshot, error) {
	defer rows.Close()
	result := make([]sourceSnapshot, 0, k)
	for rows.Next() {
		var item sourceSnapshot
		var distance float64
		if err := rows.Scan(&item.ChunkID, &item.DocumentID, &item.ChunkIndex, &item.SourceName, &item.Excerpt, &item.StartRune, &item.EndRune, &distance, &item.Hash, &item.Version); err != nil {
			return nil, err
		}
		if math.IsNaN(distance) || math.IsInf(distance, 0) {
			return nil, NewError("EMBEDDING_INVALID_VECTOR")
		}
		item.Score = max(-1.0, min(1.0, 1-distance))
		result = append(result, item)
	}
	return result, rows.Err()
}

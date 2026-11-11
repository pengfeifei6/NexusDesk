-- +goose Up
ALTER TABLE knowledge_documents ADD COLUMN task_token TEXT CHECK (task_token ~ '^[0-9a-f]{32}$');
ALTER TABLE knowledge_document_indexes ADD COLUMN task_token TEXT CHECK (task_token ~ '^[0-9a-f]{32}$');
CREATE TABLE knowledge_jobs (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 org_id BIGINT NOT NULL,
 document_id BIGINT NOT NULL,
 requested_by BIGINT NOT NULL,
 actor_user_id BIGINT NOT NULL,
 type TEXT NOT NULL DEFAULT 'DOCUMENT_INGEST' CHECK(type='DOCUMENT_INGEST'),
 status TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','RUNNING','RETRYING','SUCCEEDED','FAILED','CANCELED')),
 phase TEXT NOT NULL DEFAULT 'WAITING' CHECK(phase IN ('WAITING','EXTRACTING','INDEXING','DONE')),
 run_version BIGINT NOT NULL DEFAULT 1 CHECK(run_version>=1),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 3),
 max_attempts INTEGER NOT NULL DEFAULT 3 CHECK(max_attempts=3),
 total_attempts BIGINT NOT NULL DEFAULT 0 CHECK(total_attempts>=attempts),
 failure_code TEXT CHECK(failure_code IN ('AUTHORIZATION_REVOKED','DOCUMENT_DELETED','SOURCE_CHANGED','INVALID_TEXT','EMPTY_TEXT','TEXT_TOO_LARGE','PDF_UNSUPPORTED','DOCUMENT_STATE_CONFLICT','INDEX_STATE_CONFLICT','INDEX_CONTENT_INVALID','INDEX_TOO_MANY_CHUNKS','INDEX_SOURCE_CHANGED','EMBEDDING_COUNT_MISMATCH','EMBEDDING_DIMENSION_MISMATCH','EMBEDDING_INVALID_VECTOR','MODEL_INVALID_RESPONSE','MODEL_UPSTREAM_ERROR','MODEL_UNAVAILABLE','MODEL_RATE_LIMITED','MODEL_TIMEOUT','INDEX_TIMEOUT','RAG_BUSY','TASK_TIMEOUT','PROCESS_CANCELED','STORAGE_ERROR','LEASE_EXPIRED','INVALID_REQUEST')),
 retryable BOOLEAN NOT NULL DEFAULT false,
 source_sha256 TEXT NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 extractor_version TEXT NOT NULL DEFAULT 'utf8-v1' CHECK(extractor_version='utf8-v1'),
 embedding_model TEXT NOT NULL DEFAULT 'bge-m3:567m' CHECK(embedding_model='bge-m3:567m'),
 embedding_dimension INTEGER NOT NULL DEFAULT 1024 CHECK(embedding_dimension=1024),
 chunker_version TEXT NOT NULL DEFAULT 'rune-500-80-v1' CHECK(chunker_version='rune-500-80-v1'),
 started_at TIMESTAMPTZ,
 completed_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 next_run_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 lease_token TEXT CHECK(lease_token ~ '^[0-9a-f]{32}$'),
 lease_until TIMESTAMPTZ,
 dispatch_seq BIGINT NOT NULL DEFAULT 0 CHECK(dispatch_seq>=0),
 next_dispatch_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 last_dispatch_error TEXT CHECK(last_dispatch_error='QUEUE_UNAVAILABLE'),
 FOREIGN KEY(org_id,document_id) REFERENCES knowledge_documents(org_id,id),
 FOREIGN KEY(org_id,requested_by) REFERENCES organization_members(org_id,user_id),
 FOREIGN KEY(org_id,actor_user_id) REFERENCES organization_members(org_id,user_id),
 UNIQUE(org_id,document_id,source_sha256,extractor_version,embedding_model,chunker_version),
 CHECK((status='RUNNING' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR (status<>'RUNNING' AND lease_token IS NULL AND lease_until IS NULL)),
 CHECK((status IN ('SUCCEEDED','FAILED','CANCELED'))=(completed_at IS NOT NULL)),
 CHECK(status<>'SUCCEEDED' OR (phase='DONE' AND failure_code IS NULL AND NOT retryable))
);
CREATE INDEX knowledge_jobs_dispatch_idx ON knowledge_jobs(next_dispatch_at,next_run_at,id) WHERE status IN ('PENDING','RETRYING');
CREATE INDEX knowledge_jobs_expired_idx ON knowledge_jobs(lease_until,id) WHERE status='RUNNING';
CREATE INDEX knowledge_jobs_document_idx ON knowledge_jobs(org_id,document_id,id DESC);
-- +goose Down
DROP TABLE knowledge_jobs;
ALTER TABLE knowledge_document_indexes DROP COLUMN task_token;
ALTER TABLE knowledge_documents DROP COLUMN task_token;

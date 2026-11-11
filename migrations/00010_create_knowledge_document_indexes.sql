-- +goose Up
CREATE TABLE knowledge_document_indexes (
    document_id BIGINT PRIMARY KEY,
    org_id BIGINT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('INDEXING','READY','FAILED')),
    index_version BIGINT NOT NULL DEFAULT 1 CHECK (index_version > 0),
    embedding_model TEXT NOT NULL CHECK (embedding_model = 'bge-m3:567m'),
    embedding_dimension INTEGER NOT NULL CHECK (embedding_dimension = 1024),
    chunker_version TEXT NOT NULL CHECK (chunker_version = 'rune-500-80-v1'),
    source_content_sha256 TEXT NOT NULL CHECK (source_content_sha256 ~ '^[0-9a-f]{64}$'),
    chunk_count INTEGER NOT NULL DEFAULT 0 CHECK (chunk_count BETWEEN 0 AND 200),
    failure_code TEXT CHECK (failure_code IN (
        'INDEX_CONTENT_INVALID','INDEX_TOO_MANY_CHUNKS','INDEX_SOURCE_CHANGED',
        'EMBEDDING_COUNT_MISMATCH','EMBEDDING_DIMENSION_MISMATCH','EMBEDDING_INVALID_VECTOR',
        'MODEL_INVALID_RESPONSE','MODEL_UPSTREAM_ERROR','MODEL_UNAVAILABLE','MODEL_RATE_LIMITED',
        'MODEL_TIMEOUT','RAG_BUSY','INDEX_TIMEOUT','PROCESS_CANCELED','INDEX_FAILED')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    indexed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(org_id,document_id) REFERENCES knowledge_documents(org_id,id),
    CHECK ((status='FAILED') = (failure_code IS NOT NULL)),
    CHECK ((status='READY' AND chunk_count>0 AND indexed_at IS NOT NULL)
        OR (status<>'READY' AND chunk_count=0 AND indexed_at IS NULL))
);
CREATE INDEX knowledge_document_indexes_org_ready_idx ON knowledge_document_indexes(org_id,document_id) WHERE status='READY';

-- +goose Down
DROP TABLE knowledge_document_indexes;

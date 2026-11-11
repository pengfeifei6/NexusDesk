-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE knowledge_chunks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id BIGINT NOT NULL,
    document_id BIGINT NOT NULL,
    chunk_index INTEGER NOT NULL CHECK (chunk_index BETWEEN 0 AND 199),
    content_text TEXT NOT NULL CHECK (octet_length(content_text) BETWEEN 1 AND 2000),
    start_rune INTEGER NOT NULL CHECK (start_rune >= 0),
    end_rune INTEGER NOT NULL,
    content_sha256 TEXT NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    embedding vector(1024) NOT NULL CHECK (vector_norm(embedding) > 0),
    embedding_model TEXT NOT NULL CHECK (embedding_model = 'bge-m3:567m'),
    chunker_version TEXT NOT NULL CHECK (chunker_version = 'rune-500-80-v1'),
    index_version BIGINT NOT NULL CHECK (index_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(document_id,chunk_index),
    FOREIGN KEY(org_id,document_id) REFERENCES knowledge_documents(org_id,id),
    CHECK (end_rune > start_rune AND end_rune-start_rune <= 500),
    CHECK (char_length(content_text) = end_rune-start_rune)
);
CREATE INDEX knowledge_chunks_org_document_idx ON knowledge_chunks(org_id,document_id,index_version);

-- +goose Down
DROP TABLE knowledge_chunks;
-- The extension may be used by other tables; never drop it automatically.

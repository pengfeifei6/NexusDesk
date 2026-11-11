-- +goose Up
CREATE TABLE knowledge_document_texts (
    document_id BIGINT PRIMARY KEY,
    org_id BIGINT NOT NULL,
    content_text TEXT NOT NULL CHECK (octet_length(content_text) BETWEEN 1 AND 1048576 AND btrim(content_text)<>''),
    content_sha256 TEXT NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    extractor_version TEXT NOT NULL CHECK (extractor_version='utf8-v1'),
    extracted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(org_id,document_id) REFERENCES knowledge_documents(org_id,id)
);

-- +goose Down
DROP TABLE knowledge_document_texts;

-- +goose Up
CREATE TABLE knowledge_documents (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id),
    uploaded_by BIGINT NOT NULL,
    original_filename TEXT NOT NULL CHECK (octet_length(original_filename) BETWEEN 1 AND 200),
    storage_key TEXT NOT NULL UNIQUE CHECK (storage_key ~ '^[0-9a-f]{64}$'),
    media_type TEXT NOT NULL CHECK (media_type IN ('text/plain','text/markdown','application/pdf')),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 1 AND 67108864),
    sha256_hex TEXT NOT NULL CHECK (sha256_hex ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL DEFAULT 'UPLOADED' CHECK (status IN ('UPLOADED','PROCESSING','READY','FAILED')),
    failure_code TEXT CHECK (failure_code IN ('INVALID_TEXT','EMPTY_TEXT','TEXT_TOO_LARGE','PDF_UNSUPPORTED','SOURCE_UNAVAILABLE','SOURCE_CHANGED','PROCESS_CANCELED','PROCESS_TIMEOUT','PROCESS_FAILED')),
    extracted_chars BIGINT NOT NULL DEFAULT 0 CHECK (extracted_chars BETWEEN 0 AND 1048576),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    deleted_at TIMESTAMPTZ,
    UNIQUE(org_id,id),
    FOREIGN KEY(org_id,uploaded_by) REFERENCES organization_members(org_id,user_id),
    CHECK ((status='FAILED') = (failure_code IS NOT NULL)),
    CHECK ((status='READY' AND extracted_chars>0) OR (status<>'READY' AND extracted_chars=0))
);
CREATE INDEX knowledge_documents_org_created_idx ON knowledge_documents(org_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX knowledge_documents_org_status_idx ON knowledge_documents(org_id,status,created_at DESC,id DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE knowledge_documents;

-- +goose Up
CREATE TABLE tickets (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organizations(id),
    creator_id BIGINT NOT NULL,
    assignee_id BIGINT,
    title VARCHAR(200) NOT NULL CHECK (btrim(title) <> ''),
    body TEXT NOT NULL CHECK (btrim(body) <> '' AND char_length(body) <= 10000),
    priority SMALLINT NOT NULL DEFAULT 2 CHECK (priority BETWEEN 1 AND 3),
    status VARCHAR(20) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','IN_PROGRESS','RESOLVED','CLOSED')),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (org_id,id),
    CONSTRAINT tickets_creator_fk FOREIGN KEY(org_id,creator_id) REFERENCES organization_members(org_id,user_id),
    CONSTRAINT tickets_assignee_fk FOREIGN KEY(org_id,assignee_id) REFERENCES organization_members(org_id,user_id),
    CONSTRAINT tickets_assignment_check CHECK (status='OPEN' OR assignee_id IS NOT NULL)
);
CREATE INDEX tickets_org_created_idx ON tickets(org_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX tickets_org_status_created_idx ON tickets(org_id,status,created_at DESC,id DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE tickets;

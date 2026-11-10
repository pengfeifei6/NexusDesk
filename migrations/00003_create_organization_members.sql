-- +goose Up
CREATE TABLE organization_members (
    org_id BIGINT NOT NULL REFERENCES organizations (id),
    user_id BIGINT NOT NULL REFERENCES users (id),
    role VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT organization_members_role_check CHECK (role IN ('admin', 'member')),
    CONSTRAINT organization_members_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX organization_members_user_org_idx ON organization_members (user_id, org_id);

-- +goose Down
DROP TABLE organization_members;

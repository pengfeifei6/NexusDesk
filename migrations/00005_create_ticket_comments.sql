-- +goose Up
CREATE TABLE ticket_comments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id BIGINT NOT NULL,
    ticket_id BIGINT NOT NULL,
    author_id BIGINT NOT NULL,
    content TEXT NOT NULL CHECK (btrim(content) <> '' AND char_length(content) <= 5000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ticket_comments_ticket_fk FOREIGN KEY(org_id,ticket_id) REFERENCES tickets(org_id,id),
    CONSTRAINT ticket_comments_author_fk FOREIGN KEY(org_id,author_id) REFERENCES organization_members(org_id,user_id)
);
CREATE INDEX ticket_comments_order_idx ON ticket_comments(org_id,ticket_id,created_at,id);

-- +goose Down
DROP TABLE ticket_comments;

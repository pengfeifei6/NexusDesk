-- +goose Up
CREATE TABLE ticket_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id BIGINT NOT NULL,
    ticket_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL,
    event_type VARCHAR(30) NOT NULL CHECK (event_type IN ('TICKET_CREATED','TICKET_UPDATED','TICKET_ASSIGNED','TICKET_STATUS_CHANGED','TICKET_COMMENTED','TICKET_DELETED')),
    ticket_version BIGINT NOT NULL CHECK (ticket_version >= 0),
    before_data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(before_data)='object'),
    after_data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(after_data)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ticket_events_ticket_fk FOREIGN KEY(org_id,ticket_id) REFERENCES tickets(org_id,id),
    CONSTRAINT ticket_events_actor_fk FOREIGN KEY(org_id,actor_id) REFERENCES organization_members(org_id,user_id)
);
CREATE INDEX ticket_events_order_idx ON ticket_events(org_id,ticket_id,created_at,id);

-- +goose Down
DROP TABLE ticket_events;

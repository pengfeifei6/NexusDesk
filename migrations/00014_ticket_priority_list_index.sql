-- +goose Up
-- W09 synthetic EXPLAIN: the existing org/time index discarded 1891 rows to
-- return 20 high-priority tickets. This partial index also preserves pagination.
CREATE INDEX tickets_org_priority_created_idx ON tickets(org_id,priority,created_at DESC,id DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX tickets_org_priority_created_idx;

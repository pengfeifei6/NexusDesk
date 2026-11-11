-- +goose Up
CREATE TABLE cache_revisions (
 org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 domain TEXT NOT NULL CHECK(domain IN ('tickets','documents')),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version > 0),
 PRIMARY KEY(org_id,domain)
);

-- Generations share the original write transaction, including Worker/direct SQL
-- and AI outer transactions. No Redis invalidation can be lost after commit.
-- +goose StatementBegin
CREATE FUNCTION bump_list_cache_revision() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND OLD.org_id <> NEW.org_id) THEN
  INSERT INTO cache_revisions(org_id,domain,version) VALUES(OLD.org_id,TG_ARGV[0],1)
  ON CONFLICT(org_id,domain) DO UPDATE SET version=cache_revisions.version+1;
 END IF;
 IF TG_OP <> 'DELETE' THEN
  INSERT INTO cache_revisions(org_id,domain,version) VALUES(NEW.org_id,TG_ARGV[0],1)
  ON CONFLICT(org_id,domain) DO UPDATE SET version=cache_revisions.version+1;
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER tickets_cache_revision AFTER INSERT OR UPDATE OR DELETE ON tickets
 FOR EACH ROW EXECUTE FUNCTION bump_list_cache_revision('tickets');
CREATE TRIGGER documents_cache_revision AFTER INSERT OR UPDATE OR DELETE ON knowledge_documents
 FOR EACH ROW EXECUTE FUNCTION bump_list_cache_revision('documents');

-- +goose Down
DROP TRIGGER documents_cache_revision ON knowledge_documents;
DROP TRIGGER tickets_cache_revision ON tickets;
DROP FUNCTION bump_list_cache_revision();
DROP TABLE cache_revisions;

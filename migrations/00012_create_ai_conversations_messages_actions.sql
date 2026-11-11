-- +goose Up
CREATE TABLE ai_conversations (
 id UUID PRIMARY KEY,
 org_id BIGINT NOT NULL REFERENCES organizations(id),
 owner_user_id BIGINT NOT NULL,
 title VARCHAR(100) NOT NULL CHECK (btrim(title) <> ''),
 status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','ARCHIVED')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(org_id,id),
 FOREIGN KEY(org_id,owner_user_id) REFERENCES organization_members(org_id,user_id)
);
CREATE INDEX ai_conversations_owner_idx ON ai_conversations(org_id,owner_user_id,updated_at DESC,id DESC);
CREATE TABLE ai_messages (
 id UUID PRIMARY KEY,
 org_id BIGINT NOT NULL,
 conversation_id UUID NOT NULL,
 client_request_id UUID NOT NULL,
 role VARCHAR(16) NOT NULL CHECK(role IN ('USER','ASSISTANT')),
 content TEXT NOT NULL DEFAULT '' CHECK(octet_length(content)<=16384),
 status VARCHAR(16) NOT NULL CHECK(status IN ('STREAMING','COMPLETED','FAILED','INTERRUPTED')),
 model_name VARCHAR(100) NOT NULL DEFAULT '',
 citations JSONB NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(citations)='array'),
 input_tokens BIGINT NOT NULL DEFAULT 0 CHECK(input_tokens>=0),
 output_tokens BIGINT NOT NULL DEFAULT 0 CHECK(output_tokens>=0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 completed_at TIMESTAMPTZ,
 UNIQUE(org_id,conversation_id,id,role),
 UNIQUE(conversation_id,client_request_id,role),
 FOREIGN KEY(org_id,conversation_id) REFERENCES ai_conversations(org_id,id),
 CHECK((status='STREAMING' AND completed_at IS NULL AND role='ASSISTANT') OR (status<>'STREAMING' AND completed_at IS NOT NULL)),
 CHECK(role<>'USER' OR (status='COMPLETED' AND char_length(content) BETWEEN 1 AND 4000))
);
CREATE UNIQUE INDEX ai_messages_one_stream_idx ON ai_messages(org_id,conversation_id) WHERE status='STREAMING';
CREATE INDEX ai_messages_history_idx ON ai_messages(org_id,conversation_id,created_at,id);
CREATE TABLE ai_tool_actions (
 id UUID PRIMARY KEY,
 org_id BIGINT NOT NULL,
 conversation_id UUID NOT NULL,
 assistant_message_id UUID NOT NULL,
 assistant_role VARCHAR(16) NOT NULL DEFAULT 'ASSISTANT' CHECK(assistant_role='ASSISTANT'),
 proposed_by_user_id BIGINT NOT NULL,
 tool_name VARCHAR(40) NOT NULL CHECK(tool_name='propose_create_ticket'),
 arguments JSONB NOT NULL CHECK(jsonb_typeof(arguments)='object'),
 arguments_sha256 VARCHAR(64) NOT NULL CHECK(arguments_sha256 ~ '^[0-9a-f]{64}$'),
 status VARCHAR(16) NOT NULL DEFAULT 'PROPOSED' CHECK(status IN ('PROPOSED','EXECUTED','REJECTED','EXPIRED')),
 expires_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()+interval '15 minutes',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 confirmed_by_user_id BIGINT,
 ticket_id BIGINT,
 confirmed_at TIMESTAMPTZ,
 UNIQUE(org_id,assistant_message_id),
 FOREIGN KEY(org_id,conversation_id) REFERENCES ai_conversations(org_id,id),
 FOREIGN KEY(org_id,conversation_id,assistant_message_id,assistant_role) REFERENCES ai_messages(org_id,conversation_id,id,role),
 FOREIGN KEY(org_id,proposed_by_user_id) REFERENCES organization_members(org_id,user_id),
 FOREIGN KEY(org_id,confirmed_by_user_id) REFERENCES organization_members(org_id,user_id),
 FOREIGN KEY(org_id,ticket_id) REFERENCES tickets(org_id,id),
 CHECK((status='EXECUTED' AND confirmed_by_user_id IS NOT NULL AND ticket_id IS NOT NULL AND confirmed_at IS NOT NULL) OR (status<>'EXECUTED' AND confirmed_by_user_id IS NULL AND ticket_id IS NULL AND confirmed_at IS NULL))
);
CREATE INDEX ai_tool_actions_conversation_idx ON ai_tool_actions(org_id,conversation_id,created_at,id);
-- +goose Down
DROP TABLE ai_tool_actions;
DROP TABLE ai_messages;
DROP TABLE ai_conversations;

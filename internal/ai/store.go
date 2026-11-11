package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
)

type Store struct {
	pool    *pgxpool.Pool
	tickets *ticket.Service
}

func NewStore(pool *pgxpool.Pool, tickets *ticket.Service) *Store {
	return &Store{pool: pool, tickets: tickets}
}
func dbError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ticket.ErrNotFound) {
		return NewError("NOT_FOUND")
	}
	if errors.Is(err, ticket.ErrInvalidRequest) {
		return NewError("INVALID_REQUEST")
	}
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23505" {
		return NewError("AI_CONFLICT")
	}
	return NewError("INTERNAL_ERROR")
}
func actorValid(a Actor) error {
	if a.UserID <= 0 {
		return NewError("UNAUTHENTICATED")
	}
	if a.OrgID <= 0 {
		return NewError("INVALID_REQUEST")
	}
	return nil
}
func (s *Store) Authorize(ctx context.Context, a Actor) error {
	if err := actorValid(a); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var found int
	err := s.pool.QueryRow(ctx, `SELECT 1 FROM organization_members WHERE org_id=$1 AND user_id=$2 AND status='active' AND role IN ('admin','member')`, a.OrgID, a.UserID).Scan(&found)
	return dbError(ctx, err)
}
func (s *Store) transaction(ctx context.Context, a Actor, work func(context.Context, pgx.Tx) error) error {
	return s.transactionAuthorized(ctx, a, true, work)
}
func (s *Store) transactionAuthorized(ctx context.Context, a Actor, requireMembership bool, work func(context.Context, pgx.Tx) error) error {
	if err := actorValid(a); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbError(ctx, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='3s'`); err != nil {
		return dbError(ctx, err)
	}
	// SHARE conflicts with membership UPDATE/DELETE, making confirm and revocation serial.
	var found int
	if requireMembership {
		if err = tx.QueryRow(ctx, `SELECT 1 FROM organization_members WHERE org_id=$1 AND user_id=$2 AND status='active' AND role IN ('admin','member') FOR SHARE`, a.OrgID, a.UserID).Scan(&found); err != nil {
			return dbError(ctx, err)
		}
	}
	if err = work(ctx, tx); err != nil {
		return dbError(ctx, err)
	}
	return dbError(ctx, tx.Commit(ctx))
}

const conversationColumns = `id::text,org_id,owner_user_id,title,status,created_at,updated_at`
const messageColumns = `id::text,conversation_id::text,client_request_id::text,role,content,status,model_name,org_id,citations,input_tokens,output_tokens,created_at,completed_at`

type scanner interface{ Scan(...any) error }

func scanConversation(row scanner) (Conversation, error) {
	var v Conversation
	err := row.Scan(&v.ID, &v.OrgID, &v.OwnerUserID, &v.Title, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanMessage(row scanner) (Message, error) {
	var m Message
	var raw []byte
	err := row.Scan(&m.ID, &m.ConversationID, &m.ClientRequestID, &m.Role, &m.Content, &m.Status, &m.ModelName, &m.OrgID, &raw, &m.InputTokens, &m.OutputTokens, &m.CreatedAt, &m.CompletedAt)
	if err == nil {
		err = json.Unmarshal(raw, &m.Citations)
	}
	if m.Citations == nil {
		m.Citations = []rag.Source{}
	}
	return m, err
}
func lockConversation(ctx context.Context, tx pgx.Tx, a Actor, id string) error {
	if !ValidUUID(id) {
		return NewError("INVALID_REQUEST")
	}
	var found int
	return tx.QueryRow(ctx, `SELECT 1 FROM ai_conversations WHERE org_id=$1 AND id=$2 AND owner_user_id=$3 AND status='ACTIVE' FOR UPDATE`, a.OrgID, id, a.UserID).Scan(&found)
}
func recoverStale(ctx context.Context, tx pgx.Tx, a Actor, id string) error {
	_, err := tx.Exec(ctx, `UPDATE ai_messages SET status='INTERRUPTED',completed_at=clock_timestamp() WHERE org_id=$1 AND conversation_id=$2 AND status='STREAMING' AND created_at<clock_timestamp()-interval '120 seconds'`, a.OrgID, id)
	return err
}
func readMessages(ctx context.Context, tx pgx.Tx, a Actor, id string, limit int) ([]Message, error) {
	rows, err := tx.Query(ctx, `SELECT `+messageColumns+` FROM (SELECT * FROM ai_messages WHERE org_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3) recent ORDER BY created_at,id`, a.OrgID, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) CreateConversation(ctx context.Context, a Actor, title string) (Conversation, error) {
	title, err := clean(title, 100)
	if err != nil {
		return Conversation{}, err
	}
	id, err := newUUID()
	if err != nil {
		return Conversation{}, err
	}
	var out Conversation
	err = s.transaction(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanConversation(tx.QueryRow(ctx, `INSERT INTO ai_conversations(id,org_id,owner_user_id,title) VALUES($1,$2,$3,$4) RETURNING `+conversationColumns, id, a.OrgID, a.UserID, title))
		return err
	})
	return out, err
}
func (s *Store) ListConversations(ctx context.Context, a Actor) ([]Conversation, error) {
	out := []Conversation{}
	err := s.transaction(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+conversationColumns+` FROM ai_conversations WHERE org_id=$1 AND owner_user_id=$2 AND status='ACTIVE' ORDER BY updated_at DESC,id DESC LIMIT 50`, a.OrgID, a.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanConversation(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) History(ctx context.Context, a Actor, id string) (History, error) {
	out := History{Messages: []Message{}, Actions: []Action{}}
	err := s.transaction(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockConversation(ctx, tx, a, id); err != nil {
			return err
		}
		if err := recoverStale(ctx, tx, a, id); err != nil {
			return err
		}
		var err error
		out.Messages, err = readMessages(ctx, tx, a, id, 100)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+actionColumns+` FROM (SELECT * FROM ai_tool_actions WHERE org_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,id DESC LIMIT 100) recent ORDER BY created_at,id`, a.OrgID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, _, err := scanAction(rows)
			if err != nil {
				return err
			}
			out.Actions = append(out.Actions, v)
		}
		return rows.Err()
	})
	return out, err
}

// PreflightTurn checks input and current owner visibility without mutating history.
// StartTurn repeats its transactional checks after quota acceptance.
func (s *Store) PreflightTurn(ctx context.Context, a Actor, id, requestID, content string) error {
	if err := actorValid(a); err != nil {
		return err
	}
	if _, err := validateTurnInput(id, requestID, content); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var found int
	err := s.pool.QueryRow(ctx, `SELECT 1 FROM ai_conversations c JOIN organization_members m ON m.org_id=c.org_id AND m.user_id=c.owner_user_id WHERE c.org_id=$1 AND c.id=$2 AND c.owner_user_id=$3 AND c.status='ACTIVE' AND m.status='active' AND m.role IN ('admin','member')`, a.OrgID, id, a.UserID).Scan(&found)
	return dbError(ctx, err)
}
func validateTurnInput(id, requestID, content string) (string, error) {
	if !ValidUUID(id) || !ValidUUID(requestID) || len(content) > 16384 {
		return "", NewError("INVALID_REQUEST")
	}
	return clean(content, 4000)
}
func (s *Store) StartTurn(ctx context.Context, a Actor, id, requestID, content, modelName string) (Turn, error) {
	content, err := validateTurnInput(id, requestID, content)
	if err != nil {
		return Turn{}, err
	}
	if modelName == "" || len(modelName) > 100 || !utf8.ValidString(modelName) || strings.ContainsRune(modelName, 0) {
		return Turn{}, NewError("INVALID_REQUEST")
	}
	uid, err := newUUID()
	if err != nil {
		return Turn{}, err
	}
	aid, err := newUUID()
	if err != nil {
		return Turn{}, err
	}
	var out Turn
	err = s.transaction(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockConversation(ctx, tx, a, id); err != nil {
			return err
		}
		if err := recoverStale(ctx, tx, a, id); err != nil {
			return err
		}
		var duplicate, busy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_messages WHERE org_id=$1 AND conversation_id=$2 AND client_request_id=$3),EXISTS(SELECT 1 FROM ai_messages WHERE org_id=$1 AND conversation_id=$2 AND status='STREAMING')`, a.OrgID, id, requestID).Scan(&duplicate, &busy); err != nil {
			return err
		}
		if duplicate {
			return NewError("AI_CONFLICT")
		}
		if busy {
			return NewError("AI_BUSY")
		}
		var err error
		out.History, err = readMessages(ctx, tx, a, id, 20)
		if err != nil {
			return err
		}
		out.User, err = scanMessage(tx.QueryRow(ctx, `INSERT INTO ai_messages(id,org_id,conversation_id,client_request_id,role,content,status,completed_at) VALUES($1,$2,$3,$4,'USER',$5,'COMPLETED',clock_timestamp()) RETURNING `+messageColumns, uid, a.OrgID, id, requestID, content))
		if err != nil {
			return err
		}
		out.Assistant, err = scanMessage(tx.QueryRow(ctx, `INSERT INTO ai_messages(id,org_id,conversation_id,client_request_id,role,content,status,model_name) VALUES($1,$2,$3,$4,'ASSISTANT','','STREAMING',$5) RETURNING `+messageColumns, aid, a.OrgID, id, requestID, modelName))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE ai_conversations SET updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2`, a.OrgID, id)
		return err
	})
	return out, err
}
func (s *Store) FinishTurn(ctx context.Context, a Actor, messageID, status, content string, citations []rag.Source, inputTokens, outputTokens int64) error {
	if !ValidUUID(messageID) || !(status == "COMPLETED" || status == "FAILED" || status == "INTERRUPTED") || !utf8.ValidString(content) || strings.ContainsRune(content, 0) || len(content) > 16384 || inputTokens < 0 || outputTokens < 0 {
		return NewError("INVALID_REQUEST")
	}
	if citations == nil {
		citations = []rag.Source{}
	}
	raw, err := json.Marshal(citations)
	if err != nil || len(raw) > 65536 {
		return NewError("INVALID_REQUEST")
	}
	// Terminal cleanup writes already emitted text after revocation, without reading history.
	// Ownership is still checked under the conversation lock; successful publication requires membership.
	return s.transactionAuthorized(ctx, a, status == "COMPLETED", func(ctx context.Context, tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `SELECT conversation_id::text FROM ai_messages WHERE org_id=$1 AND id=$2 AND role='ASSISTANT'`, a.OrgID, messageID).Scan(&id); err != nil {
			return err
		}
		if err := lockConversation(ctx, tx, a, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE ai_messages SET status=$3,content=$4,citations=$5,input_tokens=$6,output_tokens=$7,completed_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND status='STREAMING' AND role='ASSISTANT'`, a.OrgID, messageID, status, content, raw, inputTokens, outputTokens)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return NewError("AI_CONFLICT")
		}
		_, err = tx.Exec(ctx, `UPDATE ai_conversations SET updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2`, a.OrgID, id)
		return err
	})
}

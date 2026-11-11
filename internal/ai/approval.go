package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
)

const actionColumns = `id::text,conversation_id::text,assistant_message_id::text,tool_name,status,org_id,proposed_by_user_id,arguments,arguments_sha256,expires_at,created_at,confirmed_by_user_id,ticket_id,confirmed_at`

func scanAction(row scanner) (Action, string, error) {
	var a Action
	var raw []byte
	var digest string
	err := row.Scan(&a.ID, &a.ConversationID, &a.AssistantMessageID, &a.ToolName, &a.Status, &a.OrgID, &a.ProposedByUserID, &raw, &digest, &a.ExpiresAt, &a.CreatedAt, &a.ConfirmedByUserID, &a.TicketID, &a.ConfirmedAt)
	if err == nil {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		err = d.Decode(&a.Arguments)
	}
	return a, digest, err
}
func argumentsDigest(a TicketArguments) ([]byte, string, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
func (s *Store) Propose(ctx context.Context, a Actor, conversationID, assistantID string, args TicketArguments) (Action, error) {
	if !ValidUUID(conversationID) || !ValidUUID(assistantID) {
		return Action{}, NewError("INVALID_REQUEST")
	}
	args, err := NormalizeArguments(args)
	if err != nil {
		return Action{}, err
	}
	raw, digest, err := argumentsDigest(args)
	if err != nil {
		return Action{}, NewError("INTERNAL_ERROR")
	}
	id, err := newUUID()
	if err != nil {
		return Action{}, err
	}
	var out Action
	err = s.transaction(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockConversation(ctx, tx, a, conversationID); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM ai_messages WHERE org_id=$1 AND conversation_id=$2 AND id=$3 AND role='ASSISTANT'`, a.OrgID, conversationID, assistantID).Scan(&status); err != nil {
			return err
		}
		if status != "STREAMING" {
			return NewError("AI_CONFLICT")
		}
		var err error
		out, _, err = scanAction(tx.QueryRow(ctx, `INSERT INTO ai_tool_actions(id,org_id,conversation_id,assistant_message_id,proposed_by_user_id,tool_name,arguments,arguments_sha256) VALUES($1,$2,$3,$4,$5,'propose_create_ticket',$6,$7) RETURNING `+actionColumns, id, a.OrgID, conversationID, assistantID, a.UserID, raw, digest))
		return err
	})
	return out, err
}

// Valid saved previews survive COMPLETED, FAILED and INTERRUPTED message states.
// Approval is an explicit owner action with fresh membership and immutable inputs;
// message completion grants no execution authority and stopping does not erase it.
func (s *Store) decide(ctx context.Context, a Actor, id string, confirm bool) (Action, error) {
	if !ValidUUID(id) {
		return Action{}, NewError("INVALID_REQUEST")
	}
	var out Action
	var decisionError error
	err := s.transaction(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		var conversationID string
		if err := tx.QueryRow(ctx, `SELECT conversation_id::text FROM ai_tool_actions WHERE org_id=$1 AND id=$2 AND proposed_by_user_id=$3`, a.OrgID, id, a.UserID).Scan(&conversationID); err != nil {
			return err
		}
		if err := lockConversation(ctx, tx, a, conversationID); err != nil {
			return err
		}
		var digest string
		var err error
		out, digest, err = scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM ai_tool_actions WHERE org_id=$1 AND id=$2 AND proposed_by_user_id=$3 FOR UPDATE`, a.OrgID, id, a.UserID))
		if err != nil {
			return err
		}
		if out.Status == "EXECUTED" {
			if confirm {
				return nil
			}
			return NewError("AI_CONFLICT")
		}
		if out.Status == "REJECTED" {
			if !confirm {
				return nil
			}
			return NewError("AI_CONFLICT")
		}
		if out.Status == "EXPIRED" {
			return NewError("AI_EXPIRED")
		}
		if out.Status != "PROPOSED" {
			return NewError("AI_CONFLICT")
		}
		var expired bool
		if err := tx.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM ai_tool_actions WHERE org_id=$1 AND id=$2`, a.OrgID, id).Scan(&expired); err != nil {
			return err
		}
		if expired {
			out.Status = "EXPIRED"
			_, err = tx.Exec(ctx, `UPDATE ai_tool_actions SET status='EXPIRED' WHERE org_id=$1 AND id=$2 AND status='PROPOSED'`, a.OrgID, id)
			decisionError = NewError("AI_EXPIRED")
			return err
		}
		if !confirm {
			out, _, err = scanAction(tx.QueryRow(ctx, `UPDATE ai_tool_actions SET status='REJECTED' WHERE org_id=$1 AND id=$2 AND status='PROPOSED' RETURNING `+actionColumns, a.OrgID, id))
			return err
		}
		normalized, err := NormalizeArguments(out.Arguments)
		if err != nil {
			return NewError("AI_CONFLICT")
		}
		_, actual, err := argumentsDigest(normalized)
		if err != nil {
			return err
		}
		if normalized != out.Arguments || subtle.ConstantTimeCompare([]byte(actual), []byte(digest)) != 1 {
			return NewError("AI_CONFLICT")
		}
		if s.tickets == nil {
			return NewError("INTERNAL_ERROR")
		}
		created, err := s.tickets.CreateInTransaction(ctx, tx, a.UserID, a.OrgID, ticket.CreateInput{Title: normalized.Title, Body: normalized.Body, Priority: normalized.Priority}, out.ID)
		if err != nil {
			return err
		}
		out, _, err = scanAction(tx.QueryRow(ctx, `UPDATE ai_tool_actions SET status='EXECUTED',ticket_id=$3,confirmed_by_user_id=$4,confirmed_at=clock_timestamp() WHERE org_id=$1 AND id=$2 AND status='PROPOSED' RETURNING `+actionColumns, a.OrgID, id, created.ID, a.UserID))
		return err
	})
	if err != nil {
		return Action{}, err
	}
	return out, decisionError
}
func (s *Store) Confirm(ctx context.Context, a Actor, id string) (Action, error) {
	return s.decide(ctx, a, id, true)
}
func (s *Store) Reject(ctx context.Context, a Actor, id string) (Action, error) {
	return s.decide(ctx, a, id, false)
}

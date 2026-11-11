package ai

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

type Actor struct {
	UserID int64 `json:"user_id"`
	OrgID  int64 `json:"org_id"`
}
type Conversation struct {
	ID          string    `json:"id"`
	OrgID       int64     `json:"org_id"`
	OwnerUserID int64     `json:"owner_user_id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Message struct {
	ID              string       `json:"id"`
	ConversationID  string       `json:"conversation_id"`
	ClientRequestID string       `json:"client_request_id"`
	Role            string       `json:"role"`
	Content         string       `json:"content"`
	Status          string       `json:"status"`
	ModelName       string       `json:"model_name"`
	OrgID           int64        `json:"org_id"`
	Citations       []rag.Source `json:"citations"`
	InputTokens     int64        `json:"input_tokens"`
	OutputTokens    int64        `json:"output_tokens"`
	CreatedAt       time.Time    `json:"created_at"`
	CompletedAt     *time.Time   `json:"completed_at"`
}
type TicketArguments struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Priority int16  `json:"priority"`
}
type Action struct {
	ID                 string          `json:"id"`
	ConversationID     string          `json:"conversation_id"`
	AssistantMessageID string          `json:"assistant_message_id"`
	ToolName           string          `json:"tool_name"`
	Status             string          `json:"status"`
	OrgID              int64           `json:"org_id"`
	ProposedByUserID   int64           `json:"proposed_by_user_id"`
	Arguments          TicketArguments `json:"arguments"`
	ExpiresAt          time.Time       `json:"expires_at"`
	CreatedAt          time.Time       `json:"created_at"`
	ConfirmedByUserID  *int64          `json:"confirmed_by_user_id"`
	TicketID           *int64          `json:"ticket_id"`
	ConfirmedAt        *time.Time      `json:"confirmed_at"`
}
type History struct {
	Messages []Message `json:"messages"`
	Actions  []Action  `json:"actions"`
}
type Turn struct {
	User      Message   `json:"user"`
	Assistant Message   `json:"assistant"`
	History   []Message `json:"history"`
}

func clean(s string, max int) (string, error) {
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", NewError("INVALID_REQUEST")
	}
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > max {
		return "", NewError("INVALID_REQUEST")
	}
	return s, nil
}
func NormalizeArguments(a TicketArguments) (TicketArguments, error) {
	var err error
	a.Title, err = clean(a.Title, 200)
	if err != nil {
		return TicketArguments{}, err
	}
	a.Body, err = clean(a.Body, 10000)
	if err != nil {
		return TicketArguments{}, err
	}
	if a.Priority < 1 || a.Priority > 3 {
		return TicketArguments{}, NewError("INVALID_REQUEST")
	}
	return a, nil
}
func ValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", NewError("INTERNAL_ERROR")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}

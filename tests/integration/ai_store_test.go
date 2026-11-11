package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pengfeifei6/NexusDesk/internal/ai"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
)

func week08Store(t *testing.T) (*knowledgeFixture, *ai.Store, ai.Actor) {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W08_AI") != "1" {
		t.Skip("requires isolated W08 harness (NEXUSDESK_TEST_W08_AI=1)")
	}
	f := week06Fixture(t)
	return f, ai.NewStore(f.pool, ticket.NewService(ticket.NewPostgresRepository(f.pool), f.members)), ai.Actor{UserID: f.uploader, OrgID: f.org}
}
func w08Code(t *testing.T, err error, code string) {
	t.Helper()
	if ai.Code(err) != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func w08Turn(t *testing.T, s *ai.Store, a ai.Actor) (ai.Conversation, ai.Turn) {
	t.Helper()
	c, e := s.CreateConversation(context.Background(), a, " Synthetic conversation ")
	if e != nil {
		t.Fatal(e)
	}
	turn, e := s.StartTurn(context.Background(), a, c.ID, "550e8400-e29b-41d4-a716-446655440000", "Synthetic question", "deepseek-flash")
	if e != nil {
		t.Fatal(e)
	}
	return c, turn
}
func w08Proposal(t *testing.T, s *ai.Store, a ai.Actor) (ai.Conversation, ai.Turn, ai.Action) {
	t.Helper()
	c, tr := w08Turn(t, s, a)
	act, e := s.Propose(context.Background(), a, c.ID, tr.Assistant.ID, ai.TicketArguments{Title: " Synthetic ticket ", Body: " Synthetic body ", Priority: 2})
	if e != nil {
		t.Fatal(e)
	}
	return c, tr, act
}
func TestWeek08StoreIsolationDedupeRecovery(t *testing.T) {
	f, s, a := week08Store(t)
	c, tr := w08Turn(t, s, a)
	for _, other := range []ai.Actor{{UserID: f.admin, OrgID: f.org}, {UserID: f.peer, OrgID: f.org}, {UserID: f.foreign, OrgID: f.otherOrg}} {
		_, e := s.History(f.ctx, other, c.ID)
		w08Code(t, e, "NOT_FOUND")
	}
	_, e := s.StartTurn(f.ctx, a, c.ID, tr.User.ClientRequestID, "duplicate", "deepseek-flash")
	w08Code(t, e, "AI_CONFLICT")
	_, e = s.StartTurn(f.ctx, a, c.ID, "550e8400-e29b-41d4-a716-446655440001", "busy", "deepseek-flash")
	w08Code(t, e, "AI_BUSY")
	f.exec(t, "UPDATE ai_messages SET created_at=clock_timestamp()-interval '121 seconds' WHERE org_id=$1 AND id=$2", a.OrgID, tr.Assistant.ID)
	h, e := s.History(f.ctx, a, c.ID)
	if e != nil || len(h.Messages) != 2 {
		t.Fatalf("stale history: %#v %v", h, e)
	}
	var recovered *ai.Message
	for i := range h.Messages {
		if h.Messages[i].ID == tr.Assistant.ID {
			recovered = &h.Messages[i]
			break
		}
	}
	if recovered == nil || recovered.Status != "INTERRUPTED" || recovered.CompletedAt == nil {
		t.Fatalf("assistant was not recovered: %#v", recovered)
	}
	next, e := s.StartTurn(f.ctx, a, c.ID, "550e8400-e29b-41d4-a716-446655440001", "next", "deepseek-flash")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishTurn(f.ctx, a, next.Assistant.ID, "COMPLETED", "保存的文字", nil, 2, 3); e != nil {
		t.Fatal(e)
	}
	h, e = s.History(f.ctx, a, c.ID)
	if e != nil || len(h.Messages) != 4 || h.Messages[3].Content != "保存的文字" || h.Messages[3].InputTokens != 2 {
		t.Fatal("finish not durable", e)
	}
	f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", a.OrgID, a.UserID)
	_, e = s.History(f.ctx, a, c.ID)
	w08Code(t, e, "NOT_FOUND")
}
func TestWeek08ConfirmExactlyOnce(t *testing.T) {
	f, s, a := week08Store(t)
	_, tr, act := w08Proposal(t, s, a)
	// A saved explicit preview remains confirmable after stop/interruption.
	if e := s.FinishTurn(f.ctx, a, tr.Assistant.ID, "INTERRUPTED", "partial", nil, 0, 0); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	out := make(chan ai.Action, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := s.Confirm(f.ctx, a, act.ID); out <- v; errs <- e }()
	}
	wg.Wait()
	close(out)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var id int64
	for v := range out {
		if v.TicketID == nil || v.Status != "EXECUTED" {
			t.Fatal("not executed")
		}
		if id != 0 && id != *v.TicketID {
			t.Fatal("duplicate ticket")
		}
		id = *v.TicketID
	}
	if n := f.count(t, "SELECT count(*) FROM tickets WHERE org_id=$1 AND id=$2 AND creator_id=$3 AND title='Synthetic ticket'", a.OrgID, id, a.UserID); n != 1 {
		t.Fatal("ticket missing")
	}
	if n := f.count(t, "SELECT count(*) FROM ticket_events WHERE org_id=$1 AND ticket_id=$2 AND event_type='TICKET_CREATED' AND after_data->>'ai_proposal_id'=$3", a.OrgID, id, act.ID); n != 1 {
		t.Fatal("audit link missing/duplicated")
	}
	_, e := s.Reject(f.ctx, a, act.ID)
	w08Code(t, e, "AI_CONFLICT")
}
func TestWeek08ActionSafetyAndRollback(t *testing.T) {
	for _, mode := range []string{"expired", "rejected", "revoked", "digest", "audit_failure", "commit_failure"} {
		t.Run(mode, func(t *testing.T) {
			f, s, a := week08Store(t)
			_, _, act := w08Proposal(t, s, a)
			want := "AI_CONFLICT"
			switch mode {
			case "expired":
				f.exec(t, "UPDATE ai_tool_actions SET expires_at=clock_timestamp()-interval '1 second' WHERE org_id=$1 AND id=$2", a.OrgID, act.ID)
				want = "AI_EXPIRED"
			case "rejected":
				if _, e := s.Reject(f.ctx, a, act.ID); e != nil {
					t.Fatal(e)
				}
				if _, e := s.Reject(f.ctx, a, act.ID); e != nil {
					t.Fatal(e)
				}
			case "revoked":
				f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", a.OrgID, a.UserID)
				want = "NOT_FOUND"
			case "digest":
				f.exec(t, `UPDATE ai_tool_actions SET arguments=jsonb_set(arguments,'{title}','"Tampered"') WHERE org_id=$1 AND id=$2`, a.OrgID, act.ID)
			case "audit_failure", "commit_failure":
				want = "INTERNAL_ERROR"
				f.exec(t, `CREATE FUNCTION nexusdesk_w08_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private fixture'; END $$`)
				ddl := `CREATE TRIGGER nexusdesk_w08_failure BEFORE INSERT ON ticket_events FOR EACH ROW EXECUTE FUNCTION nexusdesk_w08_fail()`
				if mode == "commit_failure" {
					ddl = `CREATE CONSTRAINT TRIGGER nexusdesk_w08_failure AFTER INSERT ON ticket_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nexusdesk_w08_fail()`
				}
				f.exec(t, ddl)
				t.Cleanup(func() {
					f.exec(t, `DROP TRIGGER nexusdesk_w08_failure ON ticket_events`)
					f.exec(t, `DROP FUNCTION nexusdesk_w08_fail()`)
				})
			}
			_, e := s.Confirm(f.ctx, a, act.ID)
			w08Code(t, e, want)
			if n := f.count(t, "SELECT count(*) FROM tickets WHERE org_id=$1", a.OrgID); n != 0 {
				t.Fatal("failed action created ticket")
			}
			if n := f.count(t, "SELECT count(*) FROM ai_tool_actions WHERE org_id=$1 AND id=$2 AND ticket_id IS NOT NULL", a.OrgID, act.ID); n != 0 {
				t.Fatal("failed action recorded ticket")
			}
		})
	}
}
func TestWeek08SchemaConstraints(t *testing.T) {
	f, s, a := week08Store(t)
	c, tr, act := w08Proposal(t, s, a)
	bad := func(code, q string, args ...any) {
		t.Helper()
		_, e := f.pool.Exec(f.ctx, q, args...)
		var p *pgconn.PgError
		if !errors.As(e, &p) || p.Code != code {
			t.Fatalf("want SQLSTATE %s, got %v", code, e)
		}
	}
	bad("23503", "UPDATE ai_messages SET org_id=$1 WHERE org_id=$2 AND id=$3", f.otherOrg, a.OrgID, tr.User.ID)
	bad("23503", "UPDATE ai_tool_actions SET assistant_message_id=$1 WHERE org_id=$2 AND id=$3", tr.User.ID, a.OrgID, act.ID)
	bad("23514", "UPDATE ai_messages SET status='ABSENT' WHERE org_id=$1 AND id=$2", a.OrgID, tr.Assistant.ID)
	bad("23514", "UPDATE ai_tool_actions SET status='EXECUTED' WHERE org_id=$1 AND id=$2", a.OrgID, act.ID)
	bad("23505", `INSERT INTO ai_messages(id,org_id,conversation_id,client_request_id,role,content,status,model_name,completed_at) SELECT '550e8400-e29b-41d4-a716-446655440009',org_id,conversation_id,client_request_id,role,content,status,model_name,completed_at FROM ai_messages WHERE org_id=$1 AND id=$2`, a.OrgID, tr.User.ID)
	if c.Title != "Synthetic conversation" {
		t.Fatal(fmt.Sprintf("title %q", c.Title))
	}
}
func TestWeek08MembershipRevocationSerializesConfirm(t *testing.T) {
	f, s, a := week08Store(t)
	_, _, act := w08Proposal(t, s, a)
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(f.ctx, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", a.OrgID, a.UserID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := s.Confirm(f.ctx, a, act.ID); done <- e }()
	select {
	case e := <-done:
		t.Fatalf("confirm skipped membership lock: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		w08Code(t, e, "NOT_FOUND")
	case <-time.After(6 * time.Second):
		t.Fatal("confirm did not unblock")
	}
}

func TestWeek08ConcurrentStartAndProposalUniqueness(t *testing.T) {
	f, s, a := week08Store(t)
	c, e := s.CreateConversation(f.ctx, a, "Concurrent requests")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	turns := make(chan ai.Turn, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr, e := s.StartTurn(f.ctx, a, c.ID, "550e8400-e29b-41d4-a716-446655440010", "same request", "deepseek-flash")
			results <- e
			turns <- tr
		}()
	}
	wg.Wait()
	close(results)
	close(turns)
	good, conflicts := 0, 0
	for e := range results {
		if e == nil {
			good++
		} else if ai.Code(e) == "AI_CONFLICT" {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if good != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", good, conflicts)
	}
	var assistantID string
	for tr := range turns {
		if tr.Assistant.ID != "" {
			assistantID = tr.Assistant.ID
		}
	}
	args := ai.TicketArguments{Title: "Unique preview", Body: "One action per assistant", Priority: 2}
	act, e := s.Propose(f.ctx, a, c.ID, assistantID, args)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Propose(f.ctx, a, c.ID, assistantID, args)
	w08Code(t, e, "AI_CONFLICT")
	_, e = s.Confirm(f.ctx, ai.Actor{OrgID: a.OrgID, UserID: f.admin}, act.ID)
	w08Code(t, e, "NOT_FOUND")
	_, e = s.Reject(f.ctx, ai.Actor{OrgID: f.otherOrg, UserID: f.foreign}, act.ID)
	w08Code(t, e, "NOT_FOUND")
	if n := f.count(t, "SELECT count(*) FROM ai_messages WHERE org_id=$1 AND conversation_id=$2", a.OrgID, c.ID); n != 2 {
		t.Fatal("race created extra messages")
	}
	// The partial unique index independently prevents a second live assistant.
	_, e = f.pool.Exec(f.ctx, `INSERT INTO ai_messages(id,org_id,conversation_id,client_request_id,role,status) VALUES('550e8400-e29b-41d4-a716-446655440011',$1,$2,'550e8400-e29b-41d4-a716-446655440012','ASSISTANT','STREAMING')`, a.OrgID, c.ID)
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "23505" {
		t.Fatalf("one-stream constraint missing: %v", e)
	}
}

func TestWeek08RevokedOwnerCanPersistInterruptedCleanup(t *testing.T) {
	f, s, a := week08Store(t)
	c, tr := w08Turn(t, s, a)
	f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", a.OrgID, a.UserID)
	w08Code(t, s.FinishTurn(f.ctx, a, tr.Assistant.ID, "COMPLETED", "must not publish", nil, 0, 0), "NOT_FOUND")
	if e := s.FinishTurn(f.ctx, a, tr.Assistant.ID, "INTERRUPTED", "already streamed text", nil, 0, 0); e != nil {
		t.Fatal(e)
	}
	var status, content string
	if e := f.pool.QueryRow(f.ctx, "SELECT status,content FROM ai_messages WHERE org_id=$1 AND id=$2", a.OrgID, tr.Assistant.ID).Scan(&status, &content); e != nil || status != "INTERRUPTED" || content != "already streamed text" {
		t.Fatalf("cleanup state %s %q err=%v", status, content, e)
	}
	_, e := s.History(f.ctx, a, c.ID)
	w08Code(t, e, "NOT_FOUND")
	if e := s.FinishTurn(f.ctx, ai.Actor{OrgID: a.OrgID, UserID: f.peer}, tr.Assistant.ID, "FAILED", "overwrite", nil, 0, 0); ai.Code(e) != "NOT_FOUND" {
		t.Fatal("non-owner cleanup allowed", e)
	}
}

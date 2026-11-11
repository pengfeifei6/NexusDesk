package ticket

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/organization"
)

// This boundary double persists only on Commit, so failed writes cannot look successful.
func (tx *testTx) ClaimTicket(_ context.Context, org, id, actor, expected int64) (Ticket, error) {
	if tx.s.fail == "write" {
		return Ticket{}, ErrStorage
	}
	r := tx.row
	if r.OrgID != org || r.ID != id || r.DeletedAt != nil || r.Status != Open || r.AssigneeID != nil || r.Version != expected || r.Version == math.MaxInt64 {
		return Ticket{}, ErrConflict
	}
	r.AssigneeID = &actor
	r.Version++
	tx.row = r
	return r, nil
}

func TestClaimCommitsActorVersionAndSingleEvent(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleAdmin, organization.RoleMember} {
		t.Run(string(role), func(t *testing.T) {
			s := freshStore()
			s.role = role
			got, err := NewService(s, s).Claim(context.Background(), 99, 7, 11, 0)
			if err != nil || got.AssigneeID == nil || *got.AssigneeID != 99 || got.Status != Open || got.Version != 1 || s.row.Version != 1 {
				t.Fatalf("claim identity/state/version: %v", err)
			}
			if len(s.events) != 1 || s.events[0].Type != "TICKET_ASSIGNED" || s.events[0].ActorID != 99 || s.events[0].TicketVersion != 1 {
				t.Fatal("missing or duplicate claim event")
			}
			var before, after map[string]any
			if json.Unmarshal(s.events[0].Before, &before) != nil || json.Unmarshal(s.events[0].After, &after) != nil || before["assignee_id"] != nil || before["version"] != float64(0) || after["assignee_id"] != float64(99) || after["version"] != float64(1) {
				t.Fatal("wrong claim audit identity or versions")
			}
			if _, err := NewService(s, s).Claim(context.Background(), 99, 7, 11, 1); !errors.Is(err, ErrClaimConflict) || len(s.events) != 1 || s.row.Version != 1 {
				t.Fatal("repeat claim changed state")
			}
		})
	}
}

func TestClaimRejectsUnavailableAndInvalid(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		change                  func(*testStore)
		actor, org, id, version int64
		want                    error
	}{
		{"stale", nil, 42, 7, 11, 1, ErrClaimConflict},
		{"assigned", func(s *testStore) { v := int64(99); s.row.AssigneeID = &v }, 42, 7, 11, 0, ErrClaimConflict},
		{"in_progress", func(s *testStore) { s.row.Status = InProgress }, 42, 7, 11, 0, ErrClaimConflict},
		{"resolved", func(s *testStore) { s.row.Status = Resolved }, 42, 7, 11, 0, ErrClaimConflict},
		{"closed", func(s *testStore) { s.row.Status = Closed }, 42, 7, 11, 0, ErrClaimConflict},
		{"deleted", func(s *testStore) { now := time.Now(); s.row.DeletedAt = &now }, 42, 7, 11, 0, ErrNotFound},
		{"missing", nil, 42, 7, 12, 0, ErrNotFound},
		{"other_org", nil, 42, 8, 11, 0, ErrNotFound},
		{"nonmember", nil, 100, 7, 11, 0, ErrNotFound},
		{"disabled", func(s *testStore) { s.disabledAssignee = true }, 99, 7, 11, 0, ErrNotFound},
		{"no_identity", nil, 0, 7, 11, 0, ErrUnauthenticated},
		{"negative_version", nil, 42, 7, 11, -1, ErrInvalidRequest},
		{"bad_id", nil, 42, 7, 0, 0, ErrInvalidRequest},
		{"max_version", func(s *testStore) { s.row.Version = math.MaxInt64 }, 42, 7, 11, math.MaxInt64, ErrClaimConflict},
		{"lookup_failure", func(s *testStore) { s.row.Status = Closed; s.fail = "get" }, 42, 7, 11, 0, ErrStorage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := freshStore()
			if tc.change != nil {
				tc.change(s)
			}
			before := s.row
			got, err := NewService(s, s).Claim(context.Background(), tc.actor, tc.org, tc.id, tc.version)
			if !errors.Is(err, tc.want) || got.ID != 0 || s.row.Version != before.Version || !sameID(s.row.AssigneeID, before.AssigneeID) || len(s.events) != 0 {
				t.Fatalf("rejected claim: got %v want %v", err, tc.want)
			}
		})
	}
}

func TestClaimFailuresRollback(t *testing.T) {
	for _, stage := range []string{"begin", "write", "event", "commit", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			s := freshStore()
			s.fail = stage
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ErrStorage
			if stage == "cancel" {
				s.cancelOnEvent = cancel
				want = context.Canceled
			}
			got, err := NewService(s, s).Claim(ctx, 42, 7, 11, 0)
			if !errors.Is(err, want) || got.ID != 0 || s.row.AssigneeID != nil || s.row.Version != 0 || len(s.events) != 0 || (stage != "begin" && !s.rolled) {
				t.Fatalf("claim did not roll back at %s: %v", stage, err)
			}
		})
	}
}

func TestClaimHTTPContract(t *testing.T) {
	for _, tc := range []struct {
		name, body, mime, query string
		want                    int
	}{
		{"success", `{"expected_version":0}`, "application/json", "", 200},
		{"stale", `{"expected_version":1}`, "application/json", "", 409},
		{"empty", `{}`, "application/json", "", 400},
		{"forged", `{"expected_version":0,"assignee_id":99}`, "application/json", "", 400},
		{"duplicate", `{"expected_version":0,"expected_version":0}`, "application/json", "", 400},
		{"null", `{"expected_version":null}`, "application/json", "", 400},
		{"string", `{"expected_version":"0"}`, "application/json", "", 400},
		{"fraction", `{"expected_version":0.0}`, "application/json", "", 400},
		{"exponent", `{"expected_version":0e0}`, "application/json", "", 400},
		{"negative", `{"expected_version":-1}`, "application/json", "", 400},
		{"overflow", `{"expected_version":9223372036854775808}`, "application/json", "", 400},
		{"query", `{"expected_version":0}`, "application/json", "?actor_id=99", 400},
		{"media", `{"expected_version":0}`, "text/plain", "", 415},
		{"too_large", strings.Repeat(" ", 65537), "application/json", "", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := freshStore()
			r, token := ticketRouter(t, s)
			rec := ticketRequest(r, token, "POST", "/11/claim"+tc.query, tc.body, tc.mime)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("HTTP %d want %d", rec.Code, tc.want)
			}
			if tc.want == 409 && !strings.Contains(rec.Body.String(), `"code":"TICKET_CLAIM_CONFLICT"`) {
				t.Fatal("claim conflict code missing")
			}
			if tc.want == 200 && (len(s.events) != 1 || s.row.AssigneeID == nil || *s.row.AssigneeID != 42) {
				t.Fatal("claim used untrusted actor")
			}
			if tc.want != 200 && (len(s.events) != 0 || s.row.AssigneeID != nil) {
				t.Fatal("invalid request mutated ticket")
			}
		})
	}
	s := freshStore()
	r, _ := ticketRouter(t, s)
	if rec := ticketRequest(r, "", "POST", "/11/claim", `{}`, "application/json"); rec.Code != 401 {
		t.Fatal("missing JWT not rejected")
	}
}

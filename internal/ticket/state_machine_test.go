package ticket

import (
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"testing"
)

func TestStateMachineAllEdges(t *testing.T) {
	states := []Status{Open, InProgress, Resolved, Closed}
	legal := map[[2]Status]bool{{Open, InProgress}: true, {InProgress, Resolved}: true, {Resolved, Closed}: true, {Resolved, InProgress}: true}
	for _, from := range states {
		for _, to := range states {
			err := ValidateTransition(from, to, organization.RoleAdmin, false, true)
			if legal[[2]Status{from, to}] {
				if err != nil {
					t.Fatalf("legal edge %s -> %s: %v", from, to, err)
				}
			} else if !errors.Is(err, ErrConflict) {
				t.Fatalf("illegal edge %s -> %s", from, to)
			}
		}
	}
	for _, tc := range []struct {
		from, to         Status
		role             organization.Role
		assigned, active bool
		want             error
	}{
		{Open, InProgress, organization.RoleMember, true, true, nil}, {InProgress, Resolved, organization.RoleMember, true, true, nil},
		{Open, InProgress, organization.RoleMember, false, true, ErrForbidden}, {InProgress, Resolved, organization.RoleMember, false, true, ErrForbidden},
		{Open, InProgress, organization.RoleAdmin, false, false, ErrConflict},
		{Open, InProgress, organization.RoleMember, false, false, ErrConflict}, {InProgress, Resolved, organization.RoleMember, true, false, ErrConflict},
		{Resolved, Closed, organization.RoleMember, true, true, ErrForbidden}, {Resolved, Closed, organization.RoleAdmin, false, false, nil},
		{Resolved, InProgress, organization.RoleMember, true, true, ErrForbidden}, {Resolved, InProgress, organization.RoleAdmin, false, false, ErrConflict},
		{Closed, Open, organization.RoleAdmin, true, true, ErrConflict}, {Open, "INVALID", organization.RoleAdmin, true, true, ErrInvalidRequest},
	} {
		if err := ValidateTransition(tc.from, tc.to, tc.role, tc.assigned, tc.active); !errors.Is(err, tc.want) {
			t.Fatalf("edge %s/%s role %s: %v want %v", tc.from, tc.to, tc.role, err, tc.want)
		}
	}
}

func TestObjectPoliciesAndInputLimits(t *testing.T) {
	member := organization.Membership{UserID: 42, Role: organization.RoleMember, Status: organization.StatusActive}
	ticket := Ticket{CreatorID: 42, Status: Open}
	if err := CanEdit(member, ticket, false); err != nil {
		t.Fatal(err)
	}
	if err := CanEdit(member, ticket, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("member assigned")
	}
	ticket.CreatorID = 99
	if err := CanEdit(member, ticket, false); !errors.Is(err, ErrForbidden) {
		t.Fatal("edited other creator")
	}
	member.Role = organization.RoleAdmin
	if err := CanEdit(member, ticket, true); err != nil {
		t.Fatal("admin cannot edit other OPEN")
	}
	ticket.Status = Resolved
	if err := CanEdit(member, ticket, false); !errors.Is(err, ErrConflict) {
		t.Fatal("edited nonOPEN")
	}
}

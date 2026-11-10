package organization

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memberStoreStub struct {
	role                     Role
	status                   MembershipStatus
	lookupErr, errorOnAction error
	targetID, orgID          int64
	addedRole                Role
	listPage                 PageQuery
	actions                  int
	badRecord                bool
}

func (s *memberStoreStub) FindActiveMembership(_ context.Context, org, user int64) (Membership, error) {
	if s.lookupErr != nil {
		return Membership{}, s.lookupErr
	}
	m := Membership{OrgID: org, UserID: user, Role: s.role, Status: s.status}
	if s.badRecord {
		m.OrgID++
	}
	return m, nil
}
func (s *memberStoreStub) ListMembers(_ context.Context, org int64, p PageQuery) ([]Member, error) {
	s.actions++
	s.orgID = org
	s.listPage = p
	return nil, s.errorOnAction
}
func (s *memberStoreStub) AddMember(_ context.Context, org, user int64, role Role) (Member, error) {
	s.actions++
	s.orgID = org
	s.targetID = user
	s.addedRole = role
	return Member{Membership: Membership{OrgID: org, UserID: user, Role: role, Status: StatusActive, CreatedAt: time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)}, DisplayName: "Bob"}, s.errorOnAction
}

func TestMemberServicePolicyAndOrder(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		role   Role
		status MembershipStatus
		lookup error
		write  bool
		want   error
	}{
		{"member_read", RoleMember, StatusActive, nil, false, nil}, {"admin_read", RoleAdmin, StatusActive, nil, false, nil}, {"admin_write", RoleAdmin, StatusActive, nil, true, nil},
		{"member_write", RoleMember, StatusActive, nil, true, ErrForbidden}, {"disabled_read", RoleAdmin, StatusDisabled, nil, false, ErrNotFound}, {"disabled_write", RoleAdmin, StatusDisabled, nil, true, ErrNotFound},
		{"non_member", "", "", ErrNotFound, false, ErrNotFound}, {"database_fault", "", "", ErrStorage, true, ErrStorage}, {"invalid_db_role", "support", StatusActive, nil, true, ErrStorage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &memberStoreStub{role: tc.role, status: tc.status, lookupErr: tc.lookup}
			service := NewMemberService(s)
			if err := service.Authorize(ctx, 42, 7, tc.write); !errors.Is(err, tc.want) {
				t.Fatalf("authorization error %v want %v", err, tc.want)
			}
			if tc.want != nil {
				s.errorOnAction = ErrStorage
				_, err := service.Add(ctx, 42, 7, 0, "invalid")
				want := tc.want
				if tc.name == "member_read" {
					want = ErrForbidden
				}
				if !errors.Is(err, want) || s.actions != 0 {
					t.Fatal("business input/actions ran before permission")
				}
			}
		})
	}
	store := &memberStoreStub{role: RoleAdmin, status: StatusActive}
	service := NewMemberService(store)
	added, err := service.Add(ctx, 42, 7, 99, RoleMember)
	if err != nil || added.UserID != 99 || added.Status != StatusActive || store.orgID != 7 || store.targetID != 99 || store.addedRole != RoleMember {
		t.Fatal("target or organization identity lost")
	}
	items, err := service.List(ctx, 42, 7, 2, 10)
	if err != nil || items == nil || store.orgID != 7 || store.listPage != (PageQuery{Limit: 10, Offset: 10}) {
		t.Fatal("member pagination/empty list")
	}
	for _, v := range []struct {
		id   int64
		role Role
	}{{0, RoleMember}, {-1, RoleAdmin}, {99, "support"}, {99, "ADMIN"}, {99, ""}} {
		before := store.actions
		if _, err := service.Add(ctx, 42, 7, v.id, v.role); !errors.Is(err, ErrInvalidRequest) || store.actions != before {
			t.Fatal("invalid addition reached repository")
		}
	}
	for _, v := range [][2]int64{{0, 20}, {1, 101}, {9223372036854775807, 100}} {
		before := store.actions
		if _, err := service.List(ctx, 42, 7, v[0], v[1]); !errors.Is(err, ErrInvalidRequest) || store.actions != before {
			t.Fatal("invalid page reached repository")
		}
	}
	if err := service.Authorize(ctx, 0, 7, false); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("missing trusted identity")
	}
	if err := service.Authorize(ctx, 42, 0, false); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("invalid organization identity")
	}
	store.badRecord = true
	if err := service.Authorize(ctx, 42, 7, false); !errors.Is(err, ErrStorage) {
		t.Fatal("mismatched membership accepted")
	}
	store.badRecord = false
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	before := store.actions
	if _, err := service.Add(canceled, 42, 7, 99, RoleAdmin); !errors.Is(err, context.Canceled) || before != store.actions {
		t.Fatal("canceled addition executed")
	}
	for _, fault := range []error{ErrMemberExists, ErrUserNotFound, ErrNotFound, ErrStorage} {
		store.errorOnAction = fault
		if _, err := service.Add(ctx, 42, 7, 99, RoleMember); !errors.Is(err, fault) {
			t.Fatal("lost repository failure")
		}
	}
}

package organization

import "context"

type MemberStore interface {
	FindActiveMembership(context.Context, int64, int64) (Membership, error)
	ListMembers(context.Context, int64, PageQuery) ([]Member, error)
	AddMember(context.Context, int64, int64, Role) (Member, error)
}
type MemberService struct{ store MemberStore }

func NewMemberService(store MemberStore) *MemberService { return &MemberService{store: store} }
func (s *MemberService) Authorize(ctx context.Context, actor, org int64, write bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor <= 0 {
		return ErrUnauthenticated
	}
	if org <= 0 {
		return ErrInvalidRequest
	}
	m, err := s.store.FindActiveMembership(ctx, org, actor)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.OrgID != org || m.UserID != actor {
		return ErrStorage
	}
	return membershipPermission(m, write)
}
func (s *MemberService) List(ctx context.Context, actor, org, page, size int64) ([]Member, error) {
	if err := s.Authorize(ctx, actor, org, false); err != nil {
		return nil, err
	}
	p, err := pageQuery(page, size)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListMembers(ctx, org, p)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Member{}
	}
	return items, nil
}
func (s *MemberService) Add(ctx context.Context, actor, org, target int64, role Role) (Member, error) {
	if err := s.Authorize(ctx, actor, org, true); err != nil {
		return Member{}, err
	}
	if target <= 0 || (role != RoleMember && role != RoleAdmin) {
		return Member{}, ErrInvalidRequest
	}
	return s.store.AddMember(ctx, org, target, role)
}

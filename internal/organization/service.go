package organization

import (
	"context"
	"math"
	"strings"
	"unicode/utf8"
)

type Store interface {
	CreateOrganizationWithAdmin(context.Context, int64, string) (OrganizationView, error)
	ListOrganizationsByUser(context.Context, int64, PageQuery) ([]OrganizationView, error)
}
type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }
func validName(name string) bool {
	return utf8.ValidString(name) && !strings.ContainsRune(name, 0) && utf8.RuneCountInString(name) >= 1 && utf8.RuneCountInString(name) <= 100
}
func (s *Service) Create(ctx context.Context, id int64, name string) (OrganizationView, error) {
	if err := ctx.Err(); err != nil {
		return OrganizationView{}, err
	}
	if id <= 0 {
		return OrganizationView{}, ErrUnauthenticated
	}
	name = strings.TrimSpace(name)
	if !validName(name) {
		return OrganizationView{}, ErrInvalidRequest
	}
	return s.store.CreateOrganizationWithAdmin(ctx, id, name)
}
func pageQuery(page, size int64) (PageQuery, error) {
	if page < 1 || size < 1 || size > 100 || (page-1) > math.MaxInt64/size {
		return PageQuery{}, ErrInvalidRequest
	}
	return PageQuery{Limit: int32(size), Offset: (page - 1) * size}, nil
}
func (s *Service) List(ctx context.Context, id, page, size int64) ([]OrganizationView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, ErrUnauthenticated
	}
	p, err := pageQuery(page, size)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListOrganizationsByUser(ctx, id, p)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []OrganizationView{}
	}
	return items, nil
}

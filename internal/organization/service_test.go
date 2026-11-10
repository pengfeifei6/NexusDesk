package organization

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

type storeStub struct {
	created bool
	listed  bool
	err     error
	gotID   int64
	gotName string
	gotPage PageQuery
}

func (s *storeStub) CreateOrganizationWithAdmin(ctx context.Context, id int64, name string) (OrganizationView, error) {
	s.created = true
	s.gotID = id
	s.gotName = name
	return OrganizationView{Organization: Organization{ID: 11, Name: name}, MyRole: RoleAdmin}, s.err
}
func (s *storeStub) ListOrganizationsByUser(ctx context.Context, id int64, p PageQuery) ([]OrganizationView, error) {
	s.listed = true
	s.gotID = id
	s.gotPage = p
	return nil, s.err
}

func TestOrganizationService(t *testing.T) {
	ctx := context.Background()
	store := &storeStub{}
	service := NewService(store)
	result, err := service.Create(ctx, 42, "  支持团队  ")
	if err != nil || result.Organization.Name != "支持团队" || result.MyRole != RoleAdmin || store.gotID != 42 || store.gotName != "支持团队" {
		t.Fatal("creation lost trusted identity or normalization")
	}
	if _, err := service.Create(ctx, 42, strings.Repeat("中", 100)); err != nil {
		t.Fatal("100 rune name rejected")
	}
	for _, name := range []string{"", "  ", "\x00name", string([]byte{0xff}), strings.Repeat("中", 101)} {
		store.created = false
		if _, err := service.Create(ctx, 42, name); !errors.Is(err, ErrInvalidRequest) || store.created {
			t.Fatal("invalid name reached storage")
		}
	}
	for _, id := range []int64{0, -1} {
		store.created = false
		if _, err := service.Create(ctx, id, "team"); !errors.Is(err, ErrUnauthenticated) || store.created {
			t.Fatal("untrusted creator accepted")
		}
	}
	items, err := service.List(ctx, 42, 1, 20)
	if err != nil || items == nil || len(items) != 0 || store.gotID != 42 || store.gotPage != (PageQuery{Limit: 20, Offset: 0}) {
		t.Fatal("empty first page contract")
	}
	if _, err := service.List(ctx, 42, 3, 100); err != nil || store.gotPage.Offset != 200 {
		t.Fatal("page offset")
	}
	for _, page := range [][2]int64{{0, 20}, {-1, 20}, {1, 0}, {1, 101}, {math.MaxInt64, 100}} {
		store.listed = false
		if _, err := service.List(ctx, 42, page[0], page[1]); !errors.Is(err, ErrInvalidRequest) || store.listed {
			t.Fatal("invalid pagination reached database")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	store.created = false
	store.listed = false
	if _, err := service.Create(canceled, 42, "team"); !errors.Is(err, context.Canceled) || store.created {
		t.Fatal("canceled creation")
	}
	if _, err := service.List(canceled, 42, 1, 20); !errors.Is(err, context.Canceled) || store.listed {
		t.Fatal("canceled list")
	}
	store.err = ErrStorage
	if _, err := service.Create(ctx, 42, "team"); !errors.Is(err, ErrStorage) {
		t.Fatal("lost storage error")
	}
	if _, err := service.List(ctx, 42, 1, 20); !errors.Is(err, ErrStorage) {
		t.Fatal("lost list error")
	}
}

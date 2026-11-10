package ticket

import (
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"math"
	"strings"
	"unicode/utf8"
)

func CanEdit(m organization.Membership, t Ticket, assignment bool) error {
	if m.Role != organization.RoleAdmin && (m.Role != organization.RoleMember || m.UserID != t.CreatorID || assignment) {
		return ErrForbidden
	}
	if t.Status != Open {
		return ErrConflict
	}
	return nil
}
func cleanText(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > max {
		return "", ErrInvalidRequest
	}
	return s, nil
}
func validPriority(p int16) bool { return p >= 1 && p <= 3 }
func (p Page) offset() (int64, error) {
	if p.Number < 1 || p.Size < 1 || p.Size > 100 || (p.Number-1) > math.MaxInt64/p.Size {
		return 0, ErrInvalidRequest
	}
	return (p.Number - 1) * p.Size, nil
}
func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

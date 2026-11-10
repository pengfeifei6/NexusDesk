package ticket

import "github.com/pengfeifei6/NexusDesk/internal/organization"

func validStatus(s Status) bool { return s == Open || s == InProgress || s == Resolved || s == Closed }
func ValidateTransition(from, to Status, role organization.Role, isAssignee, activeAssignee bool) error {
	if !validStatus(to) {
		return ErrInvalidRequest
	}
	switch {
	case from == Open && to == InProgress, from == InProgress && to == Resolved:
		if !activeAssignee {
			return ErrConflict
		}
		if role != organization.RoleAdmin && (role != organization.RoleMember || !isAssignee) {
			return ErrForbidden
		}
	case from == Resolved && to == Closed:
		if role != organization.RoleAdmin {
			return ErrForbidden
		}
	case from == Resolved && to == InProgress:
		if role != organization.RoleAdmin {
			return ErrForbidden
		}
		if !activeAssignee {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	return nil
}

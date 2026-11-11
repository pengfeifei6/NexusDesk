package ticket

import "errors"

var (
	ErrInvalidRequest  = errors.New("invalid request")
	ErrNotFound        = errors.New("resource not found")
	ErrForbidden       = errors.New("permission denied")
	ErrConflict        = errors.New("state or version conflict")
	ErrClaimConflict   = errors.New("ticket already assigned or version changed")
	ErrUnauthenticated = errors.New("authentication required")
	ErrInvalidAssignee = errors.New("assignee must be active in organization")
	ErrStorage         = errors.New("ticket storage failed")
)

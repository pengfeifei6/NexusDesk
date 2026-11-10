package organization

import "errors"

var (
	ErrInvalidRequest  = errors.New("invalid request")
	ErrUnauthenticated = errors.New("authentication required")
	ErrUserNotFound    = errors.New("creator no longer exists")
	ErrStorage         = errors.New("organization storage failed")
	ErrNotFound        = errors.New("resource not found")
	ErrForbidden       = errors.New("permission denied")
	ErrMemberExists    = errors.New("membership already exists")
)

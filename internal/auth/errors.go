package auth

import "errors"

// Errors are deliberately static: database and credential values never appear.
var (
	ErrInvalidRequest     = errors.New("invalid request")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrInvalidHash        = errors.New("unsupported password hash")
	ErrPasswordHash       = errors.New("password hashing failed")
	ErrAuthBusy           = errors.New("authentication service busy")
	ErrEmailExists        = errors.New("email already registered")
	ErrNotFound           = errors.New("user not found")
	ErrInvalidArgument    = errors.New("invalid repository argument")
	ErrStorage            = errors.New("user storage failed")
	ErrInvalidToken       = errors.New("invalid access token")
	ErrTokenConfig        = errors.New("invalid token configuration")
	ErrTokenIssue         = errors.New("access token issuance failed")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

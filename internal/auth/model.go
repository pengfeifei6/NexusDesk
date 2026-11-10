// Package auth implements user registration and password handling.
package auth

import "time"

type User struct {
	ID          int64
	Email       string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Credential struct {
	User         User
	PasswordHash string `json:"-"`
}

type CreateUserParams struct {
	Email        string
	DisplayName  string
	PasswordHash string `json:"-"`
}

type RegisterInput struct {
	Email       string
	DisplayName string
	Password    string `json:"-"`
}

type LoginInput struct {
	Email    string
	Password string `json:"-"`
}

// AccessToken is intentionally returned only by a successful login response.
type AccessToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

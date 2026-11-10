package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type UserCreator interface {
	CreateUser(context.Context, CreateUserParams) (User, error)
}

type PasswordHashing interface {
	HashPassword(context.Context, string) (string, error)
}

type Service struct {
	users     UserCreator
	passwords PasswordHashing
}

func NewService(users UserCreator, passwords PasswordHashing) *Service {
	return &Service{users: users, passwords: passwords}
}

func normalizeEmail(input string) (string, error) {
	email := strings.TrimSpace(input)
	if len(email) < 3 || len(email) > 254 {
		return "", ErrInvalidEmail
	}
	for _, character := range []byte(email) {
		if character <= ' ' || character >= 127 {
			return "", ErrInvalidEmail
		}
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(email), nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if strings.TrimSpace(input.Email) == "" || input.Password == "" {
		return User{}, ErrInvalidRequest
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return User{}, err
	}
	name := strings.TrimSpace(input.DisplayName)
	if !utf8.ValidString(name) || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 {
		return User{}, ErrInvalidRequest
	}
	if !validPassword(input.Password) {
		return User{}, ErrInvalidPassword
	}
	encoded, err := s.passwords.HashPassword(ctx, input.Password)
	if err != nil {
		return User{}, err
	}
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	return s.users.CreateUser(ctx, CreateUserParams{Email: email, DisplayName: name, PasswordHash: encoded})
}

type CredentialFinder interface {
	FindUserByEmail(context.Context, string) (Credential, error)
}

type LoginPasswords interface {
	PasswordHashing
	VerifyPassword(context.Context, string, string) (bool, error)
}

type TokenIssuer interface {
	Issue(int64) (AccessToken, error)
}

type LoginService struct {
	users     CredentialFinder
	passwords LoginPasswords
	tokens    TokenIssuer
	dummyHash string
}

// NewLoginService prepares one synthetic credential at startup, never per request.
// Pass the same PasswordHasher used by registration to share its resource budget.
func NewLoginService(ctx context.Context, users CredentialFinder, passwords LoginPasswords, tokens TokenIssuer) (*LoginService, error) {
	dummy, err := passwords.HashPassword(ctx, "Dummy-Login-Fixture-Never-An-Account-Password")
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dummy == "" {
		return nil, ErrPasswordHash
	}
	return &LoginService{users: users, passwords: passwords, tokens: tokens, dummyHash: dummy}, nil
}

func (s *LoginService) Login(ctx context.Context, input LoginInput) (AccessToken, error) {
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	if strings.TrimSpace(input.Email) == "" || input.Password == "" {
		return AccessToken{}, ErrInvalidRequest
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return AccessToken{}, err
	}
	if !validPassword(input.Password) {
		return AccessToken{}, ErrInvalidPassword
	}
	credential, err := s.users.FindUserByEmail(ctx, email)
	missing := errors.Is(err, ErrNotFound)
	if err != nil && !missing {
		return AccessToken{}, err
	}
	encoded := credential.PasswordHash
	if missing {
		encoded = s.dummyHash
	}
	match, err := s.passwords.VerifyPassword(ctx, input.Password, encoded)
	if err != nil {
		return AccessToken{}, err
	}
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	// Knowing the public dummy input can never authenticate a missing account.
	if missing || !match {
		return AccessToken{}, ErrInvalidCredentials
	}
	if credential.User.ID <= 0 {
		return AccessToken{}, ErrStorage
	}
	result, err := s.tokens.Issue(credential.User.ID)
	if err != nil {
		return AccessToken{}, err
	}
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	return result, nil
}

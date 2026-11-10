package auth

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
)

const supportedHashPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"

// PasswordHasher shares a bounded budget across both hashing and verification.
// Create one per process and inject the same instance into auth services.
type PasswordHasher struct {
	slots chan struct{}
}

func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{slots: make(chan struct{}, 2)}
}

func validPassword(password string) bool {
	if len(password) > 512 || !utf8.ValidString(password) || strings.TrimSpace(password) == "" {
		return false
	}
	length := utf8.RuneCountInString(password)
	return length >= 15 && length <= 128
}

func (h *PasswordHasher) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case h.slots <- struct{}{}:
		return nil
	default:
		return ErrAuthBusy
	}
}

func (h *PasswordHasher) HashPassword(ctx context.Context, password string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validPassword(password) {
		return "", ErrInvalidPassword
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-h.slots }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	params := &argon2id.Params{Memory: 19456, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	encoded, err := argon2id.CreateHash(password, params)
	if err != nil {
		return "", ErrPasswordHash
	}
	// Argon2 cannot be interrupted mid-computation. Do not use its result after cancellation.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return encoded, nil
}

func supportedHash(encoded string) bool {
	// Decode only a bounded canonical format before the library can allocate Argon2 memory.
	if len(encoded) > 256 || !strings.HasPrefix(encoded, supportedHashPrefix) {
		return false
	}
	params, _, _, err := argon2id.DecodeHash(encoded)
	return err == nil && params.Memory == 19456 && params.Iterations == 2 &&
		params.Parallelism == 1 && params.SaltLength == 16 && params.KeyLength == 32
}

func (h *PasswordHasher) VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !validPassword(password) {
		return false, ErrInvalidPassword
	}
	if !supportedHash(encoded) {
		return false, ErrInvalidHash
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-h.slots }()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	match, err := argon2id.ComparePasswordAndHash(password, encoded)
	if err != nil {
		return false, ErrInvalidHash
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return match, nil
}

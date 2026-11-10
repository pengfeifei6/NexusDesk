package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type userCreatorFunc func(context.Context, CreateUserParams) (User, error)

func (f userCreatorFunc) CreateUser(ctx context.Context, p CreateUserParams) (User, error) {
	return f(ctx, p)
}

type passwordHashFunc func(context.Context, string) (string, error)

func (f passwordHashFunc) HashPassword(ctx context.Context, password string) (string, error) {
	return f(ctx, password)
}

func TestRegisterNormalizesAndHashes(t *testing.T) {
	h := NewPasswordHasher()
	store := userCreatorFunc(func(ctx context.Context, p CreateUserParams) (User, error) {
		if p.Email != "alice+tag@example.test" || p.DisplayName != "Alice 中文" {
			t.Fatal("registration identity was not normalized")
		}
		if p.PasswordHash == fixturePassword {
			t.Fatal("plaintext reached persistence")
		}
		if ok, err := h.VerifyPassword(ctx, fixturePassword, p.PasswordHash); err != nil || !ok {
			t.Fatal("stored credential was not a verifiable password hash")
		}
		return User{ID: 7, Email: p.Email, DisplayName: p.DisplayName}, nil
	})
	user, err := NewService(store, h).Register(context.Background(), RegisterInput{
		Email: "  Alice+tag@Example.Test  ", DisplayName: "  Alice 中文  ", Password: fixturePassword,
	})
	if err != nil || user.ID != 7 || user.Email != "alice+tag@example.test" {
		t.Fatalf("valid registration failed: %v", err)
	}
}

func TestRegisterRejectsInvalidInputBeforeHashing(t *testing.T) {
	for _, tc := range []struct {
		name, email, displayName, password string
		want                               error
	}{
		{"empty_email", "", "Alice", fixturePassword, ErrInvalidRequest},
		{"invalid_email", "alice", "Alice", fixturePassword, ErrInvalidEmail},
		{"display_address", "Alice <alice@example.test>", "Alice", fixturePassword, ErrInvalidEmail},
		{"unicode_email", "用户@example.test", "Alice", fixturePassword, ErrInvalidEmail},
		{"email_whitespace", "a lice@example.test", "Alice", fixturePassword, ErrInvalidEmail},
		{"email_too_long", strings.Repeat("a", 250) + "@x.test", "Alice", fixturePassword, ErrInvalidEmail},
		{"blank_display", "alice@example.test", " \t", fixturePassword, ErrInvalidRequest},
		{"long_display", "alice@example.test", strings.Repeat("中", 81), fixturePassword, ErrInvalidRequest},
		{"invalid_display_utf8", "alice@example.test", "Alice\xff", fixturePassword, ErrInvalidRequest},
		{"display_name_nul", "alice@example.test", "A\x00B", fixturePassword, ErrInvalidRequest},
		{"short_password", "alice@example.test", "Alice", "short", ErrInvalidPassword},
		{"blank_password", "alice@example.test", "Alice", strings.Repeat(" ", 20), ErrInvalidPassword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) {
				t.Fatal("invalid registration performed a storage write")
				return User{}, nil
			}), passwordHashFunc(func(context.Context, string) (string, error) {
				t.Fatal("invalid registration performed expensive password work")
				return "", nil
			}))
			user, err := service.Register(context.Background(), RegisterInput{Email: tc.email, DisplayName: tc.displayName, Password: tc.password})
			if !errors.Is(err, tc.want) || user.ID != 0 {
				t.Fatalf("expected rejected input, got %v", err)
			}
		})
	}
}

func TestRegisterStopsOnHashAndStorageFailures(t *testing.T) {
	for _, failure := range []error{ErrAuthBusy, ErrPasswordHash, context.Canceled} {
		service := NewService(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) {
			t.Fatal("failed password operation reached storage")
			return User{}, nil
		}), passwordHashFunc(func(context.Context, string) (string, error) { return "", failure }))
		user, err := service.Register(context.Background(), RegisterInput{Email: "alice@example.test", DisplayName: "Alice", Password: fixturePassword})
		if !errors.Is(err, failure) || user.ID != 0 {
			t.Fatal("password failure was swallowed")
		}
	}
	for _, failure := range []error{ErrEmailExists, ErrStorage} {
		service := NewService(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) { return User{}, failure }),
			passwordHashFunc(func(context.Context, string) (string, error) { return "test-only-encoded-fixture", nil }))
		user, err := service.Register(context.Background(), RegisterInput{Email: "alice@example.test", DisplayName: "Alice", Password: fixturePassword})
		if !errors.Is(err, failure) || user.ID != 0 {
			t.Fatal("storage failure was reported as success")
		}
	}
}

func TestRegisterDoesNotPersistAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewService(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) {
		t.Fatal("canceled registration reached storage")
		return User{}, nil
	}), passwordHashFunc(func(context.Context, string) (string, error) {
		cancel()
		return "test-only-encoded-fixture", nil
	}))
	user, err := service.Register(ctx, RegisterInput{Email: "alice@example.test", DisplayName: "Alice", Password: fixturePassword})
	if !errors.Is(err, context.Canceled) || user.ID != 0 {
		t.Fatal("canceled hash result was used")
	}
}

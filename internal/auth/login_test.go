package auth

import (
	"context"
	"errors"
	"testing"
)

type credentialFinderFunc func(context.Context, string) (Credential, error)

func (f credentialFinderFunc) FindUserByEmail(ctx context.Context, email string) (Credential, error) {
	return f(ctx, email)
}

type loginPasswordsFake struct {
	hash   func(context.Context, string) (string, error)
	verify func(context.Context, string, string) (bool, error)
}

func (f loginPasswordsFake) HashPassword(ctx context.Context, password string) (string, error) {
	return f.hash(ctx, password)
}

func (f loginPasswordsFake) VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	return f.verify(ctx, password, encoded)
}

type tokenIssuerFunc func(int64) (AccessToken, error)

func (f tokenIssuerFunc) Issue(id int64) (AccessToken, error) { return f(id) }

func TestLoginNormalizesIdentityAndPreservesPassword(t *testing.T) {
	password := "  Long-Login-Fixture-Password  "
	service, err := NewLoginService(context.Background(), credentialFinderFunc(func(_ context.Context, email string) (Credential, error) {
		if email != "alice@example.test" {
			t.Fatal("login identity was not normalized")
		}
		return Credential{User: User{ID: 7}, PasswordHash: "stored-credential"}, nil
	}), loginPasswordsFake{
		hash: func(context.Context, string) (string, error) { return "dummy-credential", nil },
		verify: func(_ context.Context, raw, encoded string) (bool, error) {
			if raw != password || encoded != "stored-credential" {
				t.Fatal("login changed password or verified the wrong credential")
			}
			return true, nil
		},
	}, tokenIssuerFunc(func(id int64) (AccessToken, error) {
		if id != 7 {
			t.Fatal("token was issued for an untrusted user ID")
		}
		return AccessToken{AccessToken: "test-only-token", TokenType: "Bearer", ExpiresIn: 900}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(context.Background(), LoginInput{Email: " Alice@Example.Test ", Password: password})
	if err != nil || result.AccessToken == "" || result.ExpiresIn != 900 {
		t.Fatal("valid login failed")
	}
}

func TestLoginUnknownAndWrongPasswordHaveSameFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing bool
		match   bool
	}{
		{"wrong_password", false, false},
		{"unknown_email", true, false},
		{"dummy_password_matches", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hashCalls, verified := 0, 0
			service, err := NewLoginService(context.Background(), credentialFinderFunc(func(context.Context, string) (Credential, error) {
				if tc.missing {
					return Credential{}, ErrNotFound
				}
				return Credential{User: User{ID: 7}, PasswordHash: "stored-credential"}, nil
			}), loginPasswordsFake{
				hash: func(context.Context, string) (string, error) { hashCalls++; return "dummy-credential", nil },
				verify: func(_ context.Context, _, encoded string) (bool, error) {
					verified++
					if (tc.missing && encoded != "dummy-credential") || (!tc.missing && encoded != "stored-credential") {
						t.Fatal("unknown account did not use the startup dummy credential")
					}
					return tc.match, nil
				},
			}, tokenIssuerFunc(func(int64) (AccessToken, error) {
				t.Fatal("failed credentials reached token issuance")
				return AccessToken{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				result, err := service.Login(context.Background(), LoginInput{Email: "alice@example.test", Password: fixturePassword})
				if !errors.Is(err, ErrInvalidCredentials) || result.AccessToken != "" {
					t.Fatal("credential failure was not uniform")
				}
			}
			if hashCalls != 1 || verified != 2 {
				t.Fatal("failed login skipped verification or regenerated dummy per request")
			}
		})
	}
}

func TestLoginRejectsInputsAndPropagatesFailures(t *testing.T) {
	for _, tc := range []struct {
		name, email, password              string
		findErr, verifyErr, issueErr, want error
		cancelDuringVerify                 bool
	}{
		{name: "empty_password", email: "alice@example.test", want: ErrInvalidRequest},
		{name: "bad_email", email: "invalid", password: fixturePassword, want: ErrInvalidEmail},
		{name: "short_password", email: "alice@example.test", password: "short", want: ErrInvalidPassword},
		{name: "database_failure", email: "alice@example.test", password: fixturePassword, findErr: ErrStorage, want: ErrStorage},
		{name: "broken_hash", email: "alice@example.test", password: fixturePassword, verifyErr: ErrInvalidHash, want: ErrInvalidHash},
		{name: "password_busy", email: "alice@example.test", password: fixturePassword, verifyErr: ErrAuthBusy, want: ErrAuthBusy},
		{name: "signing_failure", email: "alice@example.test", password: fixturePassword, issueErr: ErrTokenIssue, want: ErrTokenIssue},
		{name: "canceled_verify", email: "alice@example.test", password: fixturePassword, cancelDuringVerify: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service, err := NewLoginService(ctx, credentialFinderFunc(func(context.Context, string) (Credential, error) {
				if tc.findErr != nil {
					return Credential{}, tc.findErr
				}
				return Credential{User: User{ID: 7}, PasswordHash: "stored-credential"}, nil
			}), loginPasswordsFake{
				hash: func(context.Context, string) (string, error) { return "dummy-credential", nil },
				verify: func(context.Context, string, string) (bool, error) {
					if tc.cancelDuringVerify {
						cancel()
					}
					return true, tc.verifyErr
				},
			}, tokenIssuerFunc(func(int64) (AccessToken, error) { return AccessToken{}, tc.issueErr }))
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Login(ctx, LoginInput{Email: tc.email, Password: tc.password})
			if !errors.Is(err, tc.want) || result.AccessToken != "" {
				t.Fatal("login input/failure was swallowed")
			}
		})
	}
}

func TestLoginSharesRegistrationPasswordBudget(t *testing.T) {
	h := NewPasswordHasher()
	service, err := NewLoginService(context.Background(), credentialFinderFunc(func(context.Context, string) (Credential, error) {
		return Credential{}, ErrNotFound
	}), h, tokenIssuerFunc(func(int64) (AccessToken, error) { return AccessToken{}, ErrTokenIssue }))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case h.slots <- struct{}{}:
		default:
			t.Fatal("password budget did not support two operations")
		}
	}
	defer func() { <-h.slots; <-h.slots }()
	if _, err := service.Login(context.Background(), LoginInput{Email: "unknown@example.test", Password: fixturePassword}); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("dummy verification bypassed the shared password budget")
	}
	registration := NewService(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) { return User{}, ErrStorage }), h)
	if _, err := registration.Register(context.Background(), RegisterInput{Email: "alice@example.test", DisplayName: "Alice", Password: fixturePassword}); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("registration did not share the login password budget")
	}
}

func TestLoginRejectsStartupHashFailureAndPreCanceledRequest(t *testing.T) {
	finder := credentialFinderFunc(func(context.Context, string) (Credential, error) {
		t.Fatal("canceled or uninitialized login queried storage")
		return Credential{}, nil
	})
	issuer := tokenIssuerFunc(func(int64) (AccessToken, error) { return AccessToken{}, ErrTokenIssue })
	passwords := loginPasswordsFake{
		hash:   func(context.Context, string) (string, error) { return "", ErrPasswordHash },
		verify: func(context.Context, string, string) (bool, error) { return false, ErrInvalidHash },
	}
	if service, err := NewLoginService(context.Background(), finder, passwords, issuer); service != nil || !errors.Is(err, ErrPasswordHash) {
		t.Fatal("dummy hash initialization failure was ignored")
	}
	passwords.hash = func(context.Context, string) (string, error) { return "dummy-credential", nil }
	service, err := NewLoginService(context.Background(), finder, passwords, issuer)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := service.Login(ctx, LoginInput{Email: "alice@example.test", Password: fixturePassword}); result.AccessToken != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("pre-canceled login reached storage or token issuance")
	}
}

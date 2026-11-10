package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

const fixturePassword = "ExampleOnly-ChooseYourOwnLongPassword"

func TestPasswordSaltAndVerification(t *testing.T) {
	h := NewPasswordHasher()
	first, err := h.HashPassword(context.Background(), fixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.HashPassword(context.Background(), fixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == fixturePassword || !strings.HasPrefix(first, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal("password was not encoded with the expected independent salted Argon2id policy")
	}
	for _, encoded := range []string{first, second} {
		if ok, err := h.VerifyPassword(context.Background(), fixturePassword, encoded); err != nil || !ok {
			t.Fatalf("correct password was rejected: %v", err)
		}
		if ok, err := h.VerifyPassword(context.Background(), "AnotherExampleOnly-WrongPassword", encoded); err != nil || ok {
			t.Fatalf("wrong password was accepted or errored: %v", err)
		}
	}
}

func TestPasswordPreservesWhitespaceAndUnicode(t *testing.T) {
	h := NewPasswordHasher()
	password := "  中文密码内容保持原文且不会被截断  "
	encoded, err := h.HashPassword(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.VerifyPassword(context.Background(), password, encoded); err != nil || !ok {
		t.Fatal("original Unicode password did not verify")
	}
	if ok, err := h.VerifyPassword(context.Background(), strings.TrimSpace(password), encoded); err != nil || ok {
		t.Fatal("trimming changed the password identity or caused an unexpected error")
	}
}

func TestPasswordPolicyBoundaries(t *testing.T) {
	h := NewPasswordHasher()
	for _, tc := range []struct {
		name, password string
		valid          bool
	}{
		{"empty", "", false},
		{"too_short", strings.Repeat("a", 14), false},
		{"whitespace", strings.Repeat(" ", 20), false},
		{"invalid_utf8", strings.Repeat("a", 15) + "\xff", false},
		{"too_many_characters", strings.Repeat("a", 129), false},
		{"too_many_bytes", strings.Repeat("🙂", 129), false},
		{"minimum", strings.Repeat("a", 15), true},
		{"maximum_unicode", strings.Repeat("🙂", 128), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := h.HashPassword(context.Background(), tc.password)
			if tc.valid {
				if err != nil || encoded == "" {
					t.Fatal("valid boundary password rejected")
				}
			} else if !errors.Is(err, ErrInvalidPassword) || encoded != "" {
				t.Fatal("invalid password accepted or returned an unsafe result")
			}
		})
	}
}

func TestPasswordPreservesEmbeddedNUL(t *testing.T) {
	h := NewPasswordHasher()
	password := "Fixture-NUL-\x00-kept-Password"
	encoded, err := h.HashPassword(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.VerifyPassword(context.Background(), password, encoded); err != nil || !ok {
		t.Fatal("password NUL was not preserved")
	}
	if ok, err := h.VerifyPassword(context.Background(), strings.ReplaceAll(password, "\x00", ""), encoded); err != nil || ok {
		t.Fatal("removing password NUL did not change credential identity")
	}
}

func TestPasswordRejectsUntrustedHashParameters(t *testing.T) {
	h := NewPasswordHasher()
	valid, err := h.HashPassword(context.Background(), fixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, encoded string }{
		{"empty", ""},
		{"plaintext", fixturePassword},
		{"huge_input", strings.Repeat("x", 1024)},
		{"unbounded_memory", strings.Replace(valid, "m=19456", "m=4294967295", 1)},
		{"unbounded_iterations", strings.Replace(valid, "t=2", "t=4294967295", 1)},
		{"zero_parallelism", strings.Replace(valid, "p=1", "p=0", 1)},
		{"wrong_version", strings.Replace(valid, "v=19", "v=16", 1)},
		{"wrong_variant", strings.Replace(valid, "argon2id", "argon2i", 1)},
		{"invalid_encoding", "$argon2id$v=19$m=19456,t=2,p=1$***$***"},
		{"short_salt_key", "$argon2id$v=19$m=19456,t=2,p=1$YQ$Yg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ok, err := h.VerifyPassword(context.Background(), fixturePassword, tc.encoded); ok || !errors.Is(err, ErrInvalidHash) {
				t.Fatal("untrusted hash encoding or cost was accepted")
			}
		})
	}
}

func TestPasswordBudgetAndCancellation(t *testing.T) {
	h := NewPasswordHasher()
	encoded, err := h.HashPassword(context.Background(), fixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	// Occupy the two real budget slots to deterministically represent active work.
	for i := 0; i < 2; i++ {
		select {
		case h.slots <- struct{}{}:
		default:
			t.Fatal("password budget did not support two simultaneous operations")
		}
	}
	if _, err := h.HashPassword(context.Background(), fixturePassword); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("excess hash work did not fail fast")
	}
	if _, err := h.VerifyPassword(context.Background(), fixturePassword, encoded); !errors.Is(err, ErrAuthBusy) {
		t.Fatal("verification bypassed the shared hash budget")
	}
	<-h.slots
	<-h.slots
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.HashPassword(ctx, fixturePassword); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled hashing did not propagate context")
	}
	if _, err := h.VerifyPassword(ctx, fixturePassword, encoded); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled verification did not propagate context")
	}
	if _, err := h.HashPassword(context.Background(), fixturePassword); err != nil {
		t.Fatal("budget was not reusable after failure")
	}
}

func BenchmarkHashPassword(b *testing.B) {
	h := NewPasswordHasher()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := h.HashPassword(context.Background(), fixturePassword); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	h := NewPasswordHasher()
	encoded, err := h.HashPassword(context.Background(), fixturePassword)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ok, err := h.VerifyPassword(context.Background(), fixturePassword, encoded); err != nil || !ok {
			b.Fatal("password verification failed")
		}
	}
}

func BenchmarkTwoConcurrentHashes(b *testing.B) {
	h := NewPasswordHasher()
	b.ReportAllocs()
	b.ResetTimer()
	var workers sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		workers.Add(1)
		go func(offset int) {
			defer workers.Done()
			for i := offset; i < b.N; i += 2 {
				if _, err := h.HashPassword(context.Background(), fixturePassword); err != nil {
					b.Error(err)
					return
				}
			}
		}(worker)
	}
	workers.Wait()
}

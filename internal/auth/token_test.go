package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var fixtureTokenTime = time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)

func fixtureTokenConfig() TokenConfig {
	return TokenConfig{Issuer: "nexusdesk", Audience: "nexusdesk-api", TTL: 15 * time.Minute, Secret: bytes.Repeat([]byte{0x42}, 32)}
}

func fixtureTokenManager(t *testing.T) *TokenManager {
	t.Helper()
	manager, err := NewTokenManager(fixtureTokenConfig())
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return fixtureTokenTime }
	return manager
}

func TestTokenRoundTripAndPublicClaims(t *testing.T) {
	manager := fixtureTokenManager(t)
	result, err := manager.Issue(42)
	if err != nil || result.TokenType != "Bearer" || result.ExpiresIn != 900 {
		t.Fatal("token issuance failed")
	}
	if id, err := manager.Verify(result.AccessToken); err != nil || id != 42 {
		t.Fatal("issued token failed verification")
	}
	parts := strings.Split(result.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatal("token does not have three signed segments")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal("token payload was not base64url")
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil || len(claims) != 5 || claims["sub"] != "42" ||
		claims["iss"] != "nexusdesk" || claims["iat"] != float64(fixtureTokenTime.Unix()) ||
		claims["exp"] != float64(fixtureTokenTime.Add(15*time.Minute).Unix()) {
		t.Fatal("token claims do not match the minimal identity contract")
	}
	manager.now = func() time.Time { return fixtureTokenTime.Add(15 * time.Minute) }
	if _, err := manager.Verify(result.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("token remained valid at its exact expiry")
	}
}

func TestTokenRejectsInvalidClaimsAndAlgorithms(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(jwt.MapClaims)
		method jwt.SigningMethod
		key    any
	}{
		{"expired", func(c jwt.MapClaims) { c["exp"] = fixtureTokenTime.Add(-time.Second).Unix() }, nil, nil},
		{"future_iat", func(c jwt.MapClaims) { c["iat"] = fixtureTokenTime.Add(time.Second).Unix() }, nil, nil},
		{"excessive_lifetime", func(c jwt.MapClaims) { c["iat"] = fixtureTokenTime.Add(-time.Hour).Unix() }, nil, nil},
		{"future_nbf", func(c jwt.MapClaims) { c["nbf"] = fixtureTokenTime.Add(time.Second).Unix() }, nil, nil},
		{"fractional_future_iat", func(c jwt.MapClaims) { c["iat"] = float64(fixtureTokenTime.Unix()) + 0.5 }, nil, nil},
		{"fractional_future_nbf", func(c jwt.MapClaims) { c["nbf"] = float64(fixtureTokenTime.Unix()) + 0.5 }, nil, nil},
		{"fractional_excess_lifetime", func(c jwt.MapClaims) { c["exp"] = float64(fixtureTokenTime.Add(15*time.Minute).Unix()) + 0.5 }, nil, nil},
		{"null_nbf", func(c jwt.MapClaims) { c["nbf"] = nil }, nil, nil},
		{"string_iat", func(c jwt.MapClaims) { c["iat"] = "1794304800" }, nil, nil},
		{"overflow_exp", func(c jwt.MapClaims) { c["exp"] = json.Number("9223372036854775808") }, nil, nil},
		{"exponent_exp", func(c jwt.MapClaims) { c["exp"] = json.Number("1794305700e0") }, nil, nil},
		{"missing_sub", func(c jwt.MapClaims) { delete(c, "sub") }, nil, nil},
		{"missing_iss", func(c jwt.MapClaims) { delete(c, "iss") }, nil, nil},
		{"missing_aud", func(c jwt.MapClaims) { delete(c, "aud") }, nil, nil},
		{"missing_iat", func(c jwt.MapClaims) { delete(c, "iat") }, nil, nil},
		{"missing_exp", func(c jwt.MapClaims) { delete(c, "exp") }, nil, nil},
		{"wrong_issuer", func(c jwt.MapClaims) { c["iss"] = "another-api" }, nil, nil},
		{"wrong_audience", func(c jwt.MapClaims) { c["aud"] = []string{"another-api"} }, nil, nil},
		{"numeric_subject", func(c jwt.MapClaims) { c["sub"] = 42 }, nil, nil},
		{"zero_subject", func(c jwt.MapClaims) { c["sub"] = "0" }, nil, nil},
		{"negative_subject", func(c jwt.MapClaims) { c["sub"] = "-1" }, nil, nil},
		{"noncanonical_subject", func(c jwt.MapClaims) { c["sub"] = "042" }, nil, nil},
		{"overflow_subject", func(c jwt.MapClaims) { c["sub"] = "9223372036854775808" }, nil, nil},
		{"wrong_key", nil, nil, bytes.Repeat([]byte{0x43}, 32)},
		{"wrong_algorithm", nil, jwt.SigningMethodHS384, nil},
		{"unsigned", nil, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"sub": "42", "iss": "nexusdesk", "aud": []string{"nexusdesk-api"},
				"iat": fixtureTokenTime.Unix(), "exp": fixtureTokenTime.Add(15 * time.Minute).Unix()}
			if tc.mutate != nil {
				tc.mutate(claims)
			}
			method, key := tc.method, tc.key
			if method == nil {
				method = jwt.SigningMethodHS256
			}
			if key == nil {
				key = fixtureTokenConfig().Secret
			}
			encoded, err := jwt.NewWithClaims(method, claims).SignedString(key)
			if err != nil {
				t.Fatal("could not construct invalid-token fixture")
			}
			if id, err := fixtureTokenManager(t).Verify(encoded); id != 0 || !errors.Is(err, ErrInvalidToken) || strings.Contains(err.Error(), encoded) {
				t.Fatal("invalid token accepted or exposed in error")
			}
		})
	}
}

func TestAcceptedTokenConfigurationCanVerifyItsOwnOutput(t *testing.T) {
	for _, label := range []string{strings.Repeat("<", 256), strings.Repeat("\x01", 256), strings.Repeat("界", 85)} {
		cfg := fixtureTokenConfig()
		cfg.Issuer, cfg.Audience = label, label
		manager, err := NewTokenManager(cfg)
		if err != nil {
			t.Fatal("valid boundary configuration rejected")
		}
		manager.now = func() time.Time { return fixtureTokenTime }
		issued, err := manager.Issue(9223372036854775807)
		if err != nil {
			t.Fatal("accepted configuration could not issue a token")
		}
		if id, err := manager.Verify(issued.AccessToken); err != nil || id != 9223372036854775807 {
			t.Fatal("accepted configuration issued a token its own verifier rejected")
		}
	}
}

func TestTokenRejectsOversizedButCorrectlySignedClaims(t *testing.T) {
	claims := jwt.MapClaims{"sub": "42", "iss": "nexusdesk", "aud": []string{"nexusdesk-api"},
		"iat": fixtureTokenTime.Unix(), "exp": fixtureTokenTime.Add(15 * time.Minute).Unix(), "padding": strings.Repeat("x", 9000)}
	encoded, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(fixtureTokenConfig().Secret)
	if err != nil {
		t.Fatal("could not construct oversized signed fixture")
	}
	if _, err := fixtureTokenManager(t).Verify(encoded); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("oversized signed token bypassed the parser budget")
	}
}

func TestTokenRejectsMalformedAndTamperedInput(t *testing.T) {
	manager := fixtureTokenManager(t)
	valid, err := manager.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid.AccessToken, ".")
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"999"}`)) + "." + parts[2]
	for _, raw := range []string{"", "not-a-token", "a.b.c", tampered, strings.Repeat("x", 8193)} {
		if id, err := manager.Verify(raw); id != 0 || !errors.Is(err, ErrInvalidToken) {
			t.Fatal("malformed token accepted")
		}
	}
	for _, id := range []int64{0, -1} {
		if _, err := manager.Issue(id); !errors.Is(err, ErrTokenIssue) {
			t.Fatal("invalid user ID was signed")
		}
	}
}

func TestTokenConfigurationAndKeyOwnership(t *testing.T) {
	for _, mutate := range []func(*TokenConfig){
		func(c *TokenConfig) { c.Secret = []byte("short") },
		func(c *TokenConfig) { c.Issuer = "" },
		func(c *TokenConfig) { c.Audience = "" },
		func(c *TokenConfig) { c.Issuer = strings.Repeat("x", 257) },
		func(c *TokenConfig) { c.Audience = "bad\xff" },
		func(c *TokenConfig) { c.TTL = time.Hour },
	} {
		cfg := fixtureTokenConfig()
		mutate(&cfg)
		if _, err := NewTokenManager(cfg); !errors.Is(err, ErrTokenConfig) {
			t.Fatal("invalid token configuration accepted")
		}
	}
	cfg := fixtureTokenConfig()
	manager, err := NewTokenManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return fixtureTokenTime }
	cfg.Secret[0] ^= 0xff
	issued, err := manager.Issue(9223372036854775807)
	if err != nil {
		t.Fatal("maximum int64 user ID was rejected")
	}
	if id, err := fixtureTokenManager(t).Verify(issued.AccessToken); err != nil || id != 9223372036854775807 {
		t.Fatal("caller mutation changed the manager signing key")
	}
}

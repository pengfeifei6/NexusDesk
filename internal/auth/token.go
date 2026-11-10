package auth

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
)

// 256-byte issuer and audience can each expand 6x under JSON HTML/control
// escaping. 8 KiB covers the full signed output while keeping parsing bounded.
const maxAccessTokenBytes = 8 * 1024

type accessClaims struct{ jwt.RegisteredClaims }

func (c *accessClaims) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ErrInvalidToken
	}
	// NumericDate otherwise truncates fractions to the library's global precision.
	// Our issuer uses integer seconds, so reject non-integer wire values before
	// the library can silently round future times or excessive lifetimes.
	for _, name := range []string{"iat", "exp", "nbf"} {
		if raw, exists := fields[name]; exists {
			if _, err := strconv.ParseInt(string(raw), 10, 64); err != nil {
				return ErrInvalidToken
			}
		}
	}
	type registered jwt.RegisteredClaims
	var decoded registered
	if err := json.Unmarshal(data, &decoded); err != nil {
		return ErrInvalidToken
	}
	c.RegisteredClaims = jwt.RegisteredClaims(decoded)
	return nil
}

type TokenConfig struct {
	Issuer   string
	Audience string
	TTL      time.Duration
	Secret   []byte `json:"-"`
}

type TokenManager struct {
	issuer   string
	audience string
	secret   []byte
	now      func() time.Time
}

func NewTokenManager(cfg TokenConfig) (*TokenManager, error) {
	if len(cfg.Secret) < 32 || cfg.TTL != 15*time.Minute ||
		strings.TrimSpace(cfg.Issuer) == "" || len(cfg.Issuer) > 256 || !utf8.ValidString(cfg.Issuer) ||
		strings.TrimSpace(cfg.Audience) == "" || len(cfg.Audience) > 256 || !utf8.ValidString(cfg.Audience) {
		return nil, ErrTokenConfig
	}
	return &TokenManager{issuer: cfg.Issuer, audience: cfg.Audience, secret: append([]byte(nil), cfg.Secret...), now: time.Now}, nil
}

func (m *TokenManager) Issue(userID int64) (AccessToken, error) {
	if userID <= 0 {
		return AccessToken{}, ErrTokenIssue
	}
	now := m.now().UTC().Truncate(time.Second)
	claims := jwt.RegisteredClaims{
		Issuer: m.issuer, Audience: jwt.ClaimStrings{m.audience}, Subject: strconv.FormatInt(userID, 10),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
	}
	encoded, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil || len(encoded) > maxAccessTokenBytes {
		return AccessToken{}, ErrTokenIssue
	}
	return AccessToken{AccessToken: encoded, TokenType: "Bearer", ExpiresIn: 900}, nil
}

func (m *TokenManager) Verify(raw string) (int64, error) {
	if len(raw) == 0 || len(raw) > maxAccessTokenBytes {
		return 0, ErrInvalidToken
	}
	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(m.issuer), jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(m.now))
	if err != nil || token == nil || !token.Valid || claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return 0, ErrInvalidToken
	}
	lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if lifetime <= 0 || lifetime > 15*time.Minute {
		return 0, ErrInvalidToken
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 || strconv.FormatInt(userID, 10) != claims.Subject {
		return 0, ErrInvalidToken
	}
	return userID, nil
}

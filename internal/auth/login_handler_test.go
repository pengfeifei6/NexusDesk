package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const validLoginJSON = `{"email":"alice@example.test","password":"ExampleOnly-ChooseYourOwnLongPassword"}`

func loginTestRouter(t *testing.T, service *LoginService) *gin.Engine {
	t.Helper()
	router := gin.New()
	NewLoginHandler(service).RegisterRoutes(router.Group("/api/v1"))
	return router
}

func TestLoginHandlerRejectsStrictInput(t *testing.T) {
	service, err := NewLoginService(context.Background(), credentialFinderFunc(func(context.Context, string) (Credential, error) {
		t.Fatal("invalid login reached user lookup")
		return Credential{}, nil
	}), loginPasswordsFake{
		hash: func(context.Context, string) (string, error) { return "dummy-credential", nil },
		verify: func(context.Context, string, string) (bool, error) {
			t.Fatal("invalid login ran verification")
			return false, nil
		},
	}, tokenIssuerFunc(func(int64) (AccessToken, error) { return AccessToken{}, ErrTokenIssue }))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, contentType, code string
		status                        int
	}{
		{"missing", `{}`, "application/json", "INVALID_REQUEST", 400},
		{"empty_password", `{"email":"alice@example.test","password":""}`, "application/json", "INVALID_REQUEST", 400},
		{"null_password", `{"email":"alice@example.test","password":null}`, "application/json", "INVALID_REQUEST", 400},
		{"unknown_field", strings.TrimSuffix(validLoginJSON, "}") + `,"role":"admin"}`, "application/json", "INVALID_REQUEST", 400},
		{"duplicate", strings.TrimSuffix(validLoginJSON, "}") + `,"email":"other@example.test"}`, "application/json", "INVALID_REQUEST", 400},
		{"trailing_object", validLoginJSON + `{}`, "application/json", "INVALID_REQUEST", 400},
		{"surrogate", strings.Replace(validLoginJSON, fixturePassword, strings.Repeat("a", 15)+`\ud800`, 1), "application/json", "INVALID_REQUEST", 400},
		{"bad_email", strings.Replace(validLoginJSON, "alice@example.test", "invalid", 1), "application/json", "INVALID_EMAIL", 400},
		{"short_password", strings.Replace(validLoginJSON, fixturePassword, "short", 1), "application/json", "INVALID_PASSWORD", 400},
		{"oversize", validLoginJSON + strings.Repeat(" ", 16384), "application/json", "REQUEST_TOO_LARGE", 413},
		{"media", validLoginJSON, "text/plain", "UNSUPPORTED_MEDIA_TYPE", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(tc.body))
			request.ContentLength = -1
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			loginTestRouter(t, service).ServeHTTP(response, request)
			var body map[string]map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != tc.status || body["error"]["code"] != tc.code {
				t.Fatalf("wrong login input response: HTTP %d", response.Code)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("login error is cacheable")
			}
		})
	}
}

func TestLoginHandlerSuccessAndFailureResponses(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		missing, match               bool
		findErr, verifyErr, issueErr error
		status                       int
		code                         string
	}{
		{name: "success", match: true, status: 200},
		{name: "wrong_password", status: 401, code: "INVALID_CREDENTIALS"},
		{name: "unknown", missing: true, status: 401, code: "INVALID_CREDENTIALS"},
		{name: "database", findErr: ErrStorage, status: 500, code: "INTERNAL_ERROR"},
		{name: "raw_database_error", findErr: errors.New("postgres://private-login-fixture"), status: 500, code: "INTERNAL_ERROR"},
		{name: "hash", verifyErr: ErrInvalidHash, status: 500, code: "INTERNAL_ERROR"},
		{name: "busy", verifyErr: ErrAuthBusy, status: 503, code: "AUTH_BUSY"},
		{name: "signing", match: true, issueErr: ErrTokenIssue, status: 500, code: "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, err := NewLoginService(context.Background(), credentialFinderFunc(func(context.Context, string) (Credential, error) {
				if tc.findErr != nil {
					return Credential{}, tc.findErr
				}
				if tc.missing {
					return Credential{}, ErrNotFound
				}
				return Credential{User: User{ID: 7}, PasswordHash: "secret-stored-fixture"}, nil
			}), loginPasswordsFake{hash: func(context.Context, string) (string, error) { return "dummy-credential", nil }, verify: func(context.Context, string, string) (bool, error) { return tc.match, tc.verifyErr }},
				tokenIssuerFunc(func(int64) (AccessToken, error) {
					if tc.issueErr != nil {
						return AccessToken{}, tc.issueErr
					}
					return AccessToken{AccessToken: "<TEST_ACCESS_TOKEN>", TokenType: "Bearer", ExpiresIn: 900}, nil
				}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(validLoginJSON))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			loginTestRouter(t, service).ServeHTTP(response, request)
			var body map[string]map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != tc.status {
				t.Fatal("unexpected login response")
			}
			if tc.status == 200 {
				if len(body) != 1 || len(body["data"]) != 3 || body["data"]["token_type"] != "Bearer" || body["data"]["expires_in"] != float64(900) {
					t.Fatal("invalid login DTO")
				}
			} else {
				if len(body) != 1 || len(body["error"]) != 3 || body["error"]["code"] != tc.code || body["error"]["request_id"] != "" {
					t.Fatal("invalid login error envelope")
				}
				if tc.status == 401 && body["error"]["message"] != "invalid email or password" {
					t.Fatal("credential failure revealed account state")
				}
			}
			if strings.Contains(response.Body.String(), fixturePassword) || strings.Contains(response.Body.String(), "secret-stored-fixture") || strings.Contains(response.Body.String(), "private-login-fixture") {
				t.Fatal("login response leaked credentials")
			}
		})
	}
}

func FuzzLoginJSON(f *testing.F) {
	for _, seed := range []string{validLoginJSON, `{}`, `null`, validLoginJSON + `{}`,
		strings.Replace(validLoginJSON, fixturePassword, strings.Repeat("a", 15)+`\ud800`, 1),
		strings.Replace(validLoginJSON, fixturePassword, strings.Repeat("a", 15)+`\ud83d\ude42`, 1)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 16384 {
			return
		}
		fields, err := decodeAuthFields([]byte(body), "email", "password")
		if err != nil {
			return
		}
		var reference map[string]string
		if json.Unmarshal([]byte(body), &reference) != nil || len(reference) != 2 || fields["email"] != reference["email"] || fields["password"] != reference["password"] {
			t.Fatal("accepted login JSON changed field values")
		}
	})
}

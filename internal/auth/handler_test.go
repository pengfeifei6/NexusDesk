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

const validRegisterJSON = `{"email":"Alice@example.test","display_name":"Alice","password":"ExampleOnly-ChooseYourOwnLongPassword"}`

func registerTestRouter(store userCreatorFunc, hasher passwordHashFunc) *gin.Engine {
	router := gin.New()
	NewHandler(NewService(store, hasher)).RegisterRoutes(router.Group("/api/v1"))
	return router
}

func TestRegisterHandlerRejectsUnsafeInputs(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body, code string
		status                        int
	}{
		{"unknown_field", "application/json", strings.TrimSuffix(validRegisterJSON, "}") + `,"role":"admin"}`, "INVALID_REQUEST", 400},
		{"wrong_case_field", "application/json", strings.Replace(validRegisterJSON, "email", "Email", 1), "INVALID_REQUEST", 400},
		{"duplicate_field", "application/json", strings.TrimSuffix(validRegisterJSON, "}") + `,"email":"bob@example.test"}`, "INVALID_REQUEST", 400},
		{"multiple_objects", "application/json", validRegisterJSON + `{}`, "INVALID_REQUEST", 400},
		{"array", "application/json", "[" + validRegisterJSON + "]", "INVALID_REQUEST", 400},
		{"null_object", "application/json", "null", "INVALID_REQUEST", 400},
		{"missing_fields", "application/json", `{}`, "INVALID_REQUEST", 400},
		{"null_field", "application/json", strings.Replace(validRegisterJSON, `"Alice"`, "null", 1), "INVALID_REQUEST", 400},
		{"wrong_type", "application/json", strings.Replace(validRegisterJSON, `"Alice"`, "101", 1), "INVALID_REQUEST", 400},
		{"empty_password", "application/json", strings.Replace(validRegisterJSON, fixturePassword, "", 1), "INVALID_REQUEST", 400},
		{"invalid_utf8", "application/json", strings.Replace(validRegisterJSON, "Alice", "Ali\xffce", 1), "INVALID_REQUEST", 400},
		{"unpaired_high_surrogate", "application/json", strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+`\ud800`, 1), "INVALID_REQUEST", 400},
		{"different_unpaired_surrogate", "application/json", strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+`\ud801`, 1), "INVALID_REQUEST", 400},
		{"unpaired_low_surrogate", "application/json", strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+`\udc00`, 1), "INVALID_REQUEST", 400},
		{"two_high_surrogates", "application/json", strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+`\ud800\ud801`, 1), "INVALID_REQUEST", 400},
		{"display_name_nul", "application/json", strings.Replace(validRegisterJSON, `"Alice"`, `"A\u0000B"`, 1), "INVALID_REQUEST", 400},
		{"malformed_json", "application/json", `{`, "INVALID_REQUEST", 400},
		{"bad_email", "application/json", strings.Replace(validRegisterJSON, "Alice@example.test", "not-an-email", 1), "INVALID_EMAIL", 400},
		{"short_password", "application/json", strings.Replace(validRegisterJSON, fixturePassword, "short", 1), "INVALID_PASSWORD", 400},
		{"oversize", "application/json", validRegisterJSON + strings.Repeat(" ", 16384), "REQUEST_TOO_LARGE", 413},
		{"wrong_media", "text/plain", validRegisterJSON, "UNSUPPORTED_MEDIA_TYPE", 415},
		{"missing_media", "", validRegisterJSON, "UNSUPPORTED_MEDIA_TYPE", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := registerTestRouter(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) {
				t.Fatal("rejected input wrote a user")
				return User{}, nil
			}), passwordHashFunc(func(context.Context, string) (string, error) {
				t.Fatal("rejected input performed password work")
				return "", nil
			}))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(tc.body))
			request.ContentLength = -1 // Exercise the byte limit without trusting Content-Length.
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var body struct {
				Error struct{ Code, Message, RequestID string } `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != tc.status || body.Error.Code != tc.code {
				t.Fatalf("unexpected rejected-input response: HTTP %d, body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("error response was cacheable")
			}
		})
	}
}

func TestRegisterPreservesValidJSONUnicodeEscapes(t *testing.T) {
	for _, tc := range []struct{ name, encoded, decoded string }{
		{"valid_surrogate_pair", `\ud83d\ude42`, "🙂"},
		{"explicit_replacement_character", `\ufffd`, "�"},
		{"literal_backslash_u", `\\ud800`, `\ud800`},
		{"password_nul", `\u0000`, "\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := registerTestRouter(userCreatorFunc(func(_ context.Context, p CreateUserParams) (User, error) {
				return User{ID: 7, Email: p.Email, DisplayName: p.DisplayName}, nil
			}), passwordHashFunc(func(_ context.Context, password string) (string, error) {
				if password != strings.Repeat("a", 15)+tc.decoded {
					t.Fatal("valid password Unicode was changed by JSON decoding")
				}
				return "test-only-encoded-fixture", nil
			}))
			body := strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+tc.encoded, 1)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 201 {
				t.Fatalf("valid JSON Unicode request rejected: HTTP %d", response.Code)
			}
		})
	}
}

func TestRegisterHandlerReturnsOnlyPublicFieldsAtSizeBoundary(t *testing.T) {
	router := registerTestRouter(userCreatorFunc(func(_ context.Context, p CreateUserParams) (User, error) {
		return User{ID: 7, Email: p.Email, DisplayName: p.DisplayName}, nil
	}), passwordHashFunc(func(context.Context, string) (string, error) { return "secret-test-hash", nil }))
	body := validRegisterJSON + strings.Repeat(" ", 16384-len(validRegisterJSON))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var got map[string]map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || response.Code != 201 {
		t.Fatalf("valid request at 16 KiB was rejected: HTTP %d", response.Code)
	}
	if len(got) != 1 || len(got["data"]) != 3 || string(got["data"]["id"]) != "7" ||
		string(got["data"]["email"]) != `"alice@example.test"` || string(got["data"]["display_name"]) != `"Alice"` {
		t.Fatal("registration did not return the exact public DTO")
	}
	if strings.Contains(response.Body.String(), fixturePassword) || strings.Contains(response.Body.String(), "secret-test-hash") {
		t.Fatal("credential leaked into successful JSON")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("success response was cacheable")
	}
}

func TestRegisterHandlerMapsFailuresSafely(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		failure    error
		hashFail   bool
		status     int
	}{
		{"duplicate", "EMAIL_ALREADY_EXISTS", ErrEmailExists, false, 409},
		{"storage", "INTERNAL_ERROR", errors.New("postgres://private-fixture-secret"), false, 500},
		{"hash", "INTERNAL_ERROR", ErrPasswordHash, true, 500},
		{"busy", "AUTH_BUSY", ErrAuthBusy, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := registerTestRouter(userCreatorFunc(func(context.Context, CreateUserParams) (User, error) {
				return User{}, tc.failure
			}), passwordHashFunc(func(context.Context, string) (string, error) {
				if tc.hashFail {
					return "", tc.failure
				}
				return "secret-test-hash", nil
			}))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(validRegisterJSON))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var body map[string]map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != tc.status || body["error"]["code"] != tc.code {
				t.Fatalf("wrong error mapping: HTTP %d", response.Code)
			}
			if len(body) != 1 || len(body["error"]) != 3 || body["error"]["request_id"] != "" ||
				strings.Contains(response.Body.String(), "private-fixture-secret") || strings.Contains(response.Body.String(), fixturePassword) ||
				strings.Contains(response.Body.String(), "secret-test-hash") {
				t.Fatal("error response leaked details or broke the envelope")
			}
		})
	}
}

func FuzzDecodeRegister(f *testing.F) {
	for _, seed := range []string{
		validRegisterJSON,
		strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+`\ud800`, 1),
		strings.Replace(validRegisterJSON, fixturePassword, strings.Repeat("a", 15)+`\ud83d\ude42`, 1),
		`{"email":"a@example.test","display_name":"Alice","password":"long-valid-password","email":"b@example.test"}`,
		`null`, `{`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 16384 {
			return
		}
		input, err := decodeRegister([]byte(body))
		if err != nil {
			return
		}
		var reference map[string]string
		if err := json.Unmarshal([]byte(body), &reference); err != nil || len(reference) != 3 ||
			input.Email != reference["email"] || input.DisplayName != reference["display_name"] || input.Password != reference["password"] {
			t.Fatal("accepted registration JSON changed its decoded field values")
		}
	})
}

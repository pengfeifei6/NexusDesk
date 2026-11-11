package integration

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// comparableCorrelatedError preserves the full decoded error response except
// its independently validated per-request identity. Callers compare every
// remaining field and retain their status, authorization and side-effect checks.
func comparableCorrelatedError(response *httptest.ResponseRecorder, seen map[string]bool) (map[string]any, error) {
	var body map[string]any
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body) != 1 {
		return nil, errors.New("invalid error envelope")
	}
	failure, ok := body["error"].(map[string]any)
	if !ok || len(failure) != 3 {
		return nil, errors.New("invalid error shape")
	}
	if _, ok := failure["code"].(string); !ok {
		return nil, errors.New("invalid error code")
	}
	if _, ok := failure["message"].(string); !ok {
		return nil, errors.New("invalid error message")
	}
	id, ok := failure["request_id"].(string)
	if !ok || len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return nil, errors.New("invalid request identity")
	}
	if len(response.Header().Values("X-Request-ID")) != 1 || response.Header().Get("X-Request-ID") != id {
		return nil, errors.New("request identity header/body mismatch")
	}
	if seen[id] {
		return nil, errors.New("request identity reused")
	}
	seen[id] = true
	delete(failure, "request_id")
	return body, nil
}

func TestComparableCorrelatedErrorPreservesSemanticResponse(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range []string{"0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"} {
		response := httptest.NewRecorder()
		response.Header().Set("X-Request-ID", id)
		response.Body.WriteString(`{"error":{"code":"NOT_FOUND","message":"resource not found","request_id":"` + id + `"}}`)
		body, err := comparableCorrelatedError(response, seen)
		want := map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "resource not found"}}
		if err != nil || !reflect.DeepEqual(body, want) {
			t.Fatal("normalization changed semantic error fields", body, err)
		}
	}
}
func TestComparableCorrelatedErrorRejectsInvalidCorrelationAndShape(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, id, header, body string
		reused                 bool
	}{
		{name: "empty", id: "", header: ""},
		{name: "uppercase", id: "ABCDEF0123456789abcdef0123456789", header: "ABCDEF0123456789abcdef0123456789"},
		{name: "short", id: "0123", header: "0123"},
		{name: "nonhex", id: "z123456789abcdef0123456789abcdef", header: "z123456789abcdef0123456789abcdef"},
		{name: "missing_header", id: valid},
		{name: "mismatched_header", id: valid, header: "fedcba9876543210fedcba9876543210"},
		{name: "reused", id: valid, header: valid, reused: true},
		{name: "extra_top_level", id: valid, header: valid, body: `{"error":{"code":"NOT_FOUND","message":"resource not found","request_id":"` + valid + `"},"private":"unexpected"}`},
		{name: "extra_error_field", id: valid, header: valid, body: `{"error":{"code":"NOT_FOUND","message":"resource not found","request_id":"` + valid + `","private":"unexpected"}}`},
		{name: "nonstring_code", id: valid, header: valid, body: `{"error":{"code":404,"message":"resource not found","request_id":"` + valid + `"}}`},
		{name: "missing_message", id: valid, header: valid, body: `{"error":{"code":"NOT_FOUND","request_id":"` + valid + `"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			if tc.header != "" {
				response.Header().Set("X-Request-ID", tc.header)
			}
			body := tc.body
			if body == "" {
				body = `{"error":{"code":"NOT_FOUND","message":"resource not found","request_id":"` + tc.id + `"}}`
			}
			response.Body.WriteString(body)
			seen := map[string]bool{}
			if tc.reused {
				seen[tc.id] = true
			}
			if _, err := comparableCorrelatedError(response, seen); err == nil {
				t.Fatal("invalid error/correlation accepted")
			}
		})
	}
}

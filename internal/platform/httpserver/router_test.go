package httpserver

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	NewRouter().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header().Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		t.Fatalf("expected JSON Content-Type, got %q", response.Header().Get("Content-Type"))
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("unexpected health response: %v", body)
	}
}

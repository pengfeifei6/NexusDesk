package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestDeepSeekUsageDoesNotInvalidateGroundedAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		known       bool
	}{
		{"missing", "", false}, {"null", "null", false}, {"valid", `{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_cache_hit_tokens":3,"prompt_cache_miss_tokens":7}`, true},
		{"no_cache", `{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}`, true},
		{"negative", `{"prompt_tokens":-1,"completion_tokens":4,"total_tokens":3}`, false},
		{"float", `{"prompt_tokens":1.5,"completion_tokens":4,"total_tokens":5.5}`, false},
		{"string", `{"prompt_tokens":"10","completion_tokens":4,"total_tokens":14}`, false},
		{"overflow", `{"prompt_tokens":9223372036854775807,"completion_tokens":4,"total_tokens":3}`, false},
		{"inconsistent", `{"prompt_tokens":10,"completion_tokens":4,"total_tokens":99}`, false},
		{"cache_mismatch", `{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_cache_hit_tokens":3,"prompt_cache_miss_tokens":8}`, false},
		{"wrong_shape", `[]`, false}, {"huge", `{"prompt_tokens":1e400,"completion_tokens":4,"total_tokens":14}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			json.Unmarshal([]byte(cloudBody(cloudAnswer)), &body)
			if tc.usage != "" {
				body["usage"] = json.RawMessage(tc.usage)
			}
			raw, _ := json.Marshal(body)
			c := deepSeekFor(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, string(raw)) })
			got, err := c.Generate(context.Background(), "question", cloudSources())
			if err != nil || got.Answer != "5432" || got.Usage.Known != tc.known {
				t.Fatalf("answer=%+v err=%v", got, err)
			}
			if tc.known {
				if got.Usage.TotalTokens == nil || *got.Usage.TotalTokens != 14 {
					t.Fatal("missing measured total")
				}
			} else if got.Usage.TotalTokens != nil {
				t.Fatal("invented usage")
			}
		})
	}
}
func TestOllamaUsageTrustsOnlyNonnegativeIntegerCounts(t *testing.T) {
	for _, tc := range []struct {
		prompt, completion string
		known              bool
		total              int64
	}{{"5", "4", true, 9}, {"0", "0", true, 0}, {"null", "4", false, 0}, {" null ", "4", false, 0}, {"1.5", "4", false, 0}, {"-1", "4", false, 0}, {`"5"`, "4", false, 0}, {"9223372036854775807", "1", false, 0}} {
		got := ollamaUsage(json.RawMessage(tc.prompt), json.RawMessage(tc.completion))
		if got.Known != tc.known || tc.known && (got.TotalTokens == nil || *got.TotalTokens != tc.total) || !tc.known && got.TotalTokens != nil {
			t.Fatalf("%+v -> %+v", tc, got)
		}
	}
}

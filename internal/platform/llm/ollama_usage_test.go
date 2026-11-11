package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestChatUsageEnvelopePreservesAnswerOnMalformedCounts(t *testing.T) {
	for _, tc := range []struct {
		name               string
		prompt, completion any
		known              bool
	}{{"valid", 5, 4, true}, {"zero", 0, 0, true}, {"missing", nil, nil, false}, {"negative", -1, 4, false}, {"fraction", 1.5, 4, false}, {"string", "5", 4, false}, {"overflow", json.Number("9223372036854775807"), 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				body := map[string]any{"model": "qwen3.5:35b", "message": map[string]any{"role": "assistant", "content": `{"answer":"answer","citation_ids":["9007199254740993"],"insufficient_evidence":false}`}, "done": true, "done_reason": "stop"}
				if tc.prompt != nil {
					body["prompt_eval_count"] = tc.prompt
				}
				if tc.completion != nil {
					body["eval_count"] = tc.completion
				}
				json.NewEncoder(w).Encode(body)
			})
			got, err := c.Generator().Generate(context.Background(), "question", sources())
			if err != nil || got.Answer != "answer" || got.Usage.Known != tc.known || got.Model != "qwen3.5:35b" {
				t.Fatalf("%+v %v", got, err)
			}
			if !tc.known && (got.Usage.PromptTokens != nil || got.Usage.CompletionTokens != nil || got.Usage.TotalTokens != nil) {
				t.Fatal("unknown counts must be null")
			}
		})
	}
}

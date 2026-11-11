package rag

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestRetrievalModeHTTPContract(t *testing.T) {
	for _, mode := range []string{`""`, `null`, `"Hybrid"`, `" hybrid "`, `1`, `"keyword"`} {
		s, _, _, e, _ := fixture()
		res := request(testRouter(s, true), "POST", "/api/v1/organizations/2/knowledge/search", `{"query":"x","retrieval_mode":`+mode+`}`)
		if res.Code != 400 || e.calls != 0 {
			t.Fatalf("mode=%s status=%d calls=%d", mode, res.Code, e.calls)
		}
	}
	for _, suffix := range []string{"", `,"retrieval_mode":"vector"`} {
		s, _, _, _, _ := fixture()
		res := request(testRouter(s, true), "POST", "/api/v1/organizations/2/knowledge/search", `{"query":"x"`+suffix+`}`)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"retrieval_mode":"vector"`) {
			t.Fatalf("%d %s", res.Code, res.Body)
		}
	}
}
func TestAskUsageDistinguishesSkippedGenerationFromUnknown(t *testing.T) {
	for _, withRows := range []bool{false, true} {
		s, r, _, _, _ := fixture()
		if withRows {
			r.rows = evidence()
		}
		got, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q"})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(got)
		var payload struct {
			Usage struct {
				Known      bool   `json:"known"`
				Called     bool   `json:"model_called"`
				Prompt     *int64 `json:"prompt_tokens"`
				Completion *int64 `json:"completion_tokens"`
				Total      *int64 `json:"total_tokens"`
			} `json:"usage"`
			Timings map[string]int64 `json:"timings"`
		}
		if err = json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Usage.Called != withRows || payload.Usage.Known == withRows || len(payload.Timings) != 4 {
			t.Fatalf("%s", raw)
		}
		if !withRows && (payload.Usage.Total == nil || *payload.Usage.Total != 0) || withRows && payload.Usage.Total != nil {
			t.Fatalf("%s", raw)
		}
	}
}
func TestHybridTermsAndDeterministicRRF(t *testing.T) {
	if got := queryTerms("AUTH_KEY_42 auth_key_42 知识库，跨组织 a-b 端"); !reflect.DeepEqual(got, []string{"auth_key_42", "知识", "识库", "跨组", "组织", "a", "b"}) {
		t.Fatalf("terms=%v", got)
	}
	if got := queryTerms(strings.Repeat("重复", 100)); !reflect.DeepEqual(got, []string{"重复", "复重"}) {
		t.Fatalf("duplicates=%v", got)
	}
	words := make([]string, 40)
	for i := range words {
		words[i] = strings.Repeat("x", i+1)
	}
	if len(queryTerms(strings.Join(words, " "))) != 32 {
		t.Fatal("unbounded terms")
	}
	a := sourceSnapshot{Source: Source{ChunkID: 2, Score: .8}, Hash: "h", Version: 1}
	b := sourceSnapshot{Source: Source{ChunkID: 1, Score: .7}, Hash: "b", Version: 2}
	got := fuseSources([]sourceSnapshot{a, b}, []sourceSnapshot{b, a}, 2)
	if len(got) != 2 || got[0].ChunkID != 1 || got[1].ChunkID != 2 || got[0].Score != .7 || got[0].Hash != "b" || got[0].FusionScore == nil || math.Abs(*got[0].FusionScore-(1.0/61+1.0/62)) > 1e-12 {
		t.Fatalf("fusion=%+v", got)
	}
	if len(fuseSources([]sourceSnapshot{a, a}, []sourceSnapshot{a}, 1)) != 1 {
		t.Fatal("duplicate IDs")
	}
}

// Only the SQL boundary is replaced: service normalization, fusion, source
// revalidation, and public response construction remain real.
type hybridRepositoryFixture struct {
	*testRepository
	lexical []sourceSnapshot
	org     int64
	k       int
	minimum float64
	terms   []string
}

func (r *hybridRepositoryFixture) LexicalSearch(_ context.Context, org int64, _ []float32, terms []string, k int, minimum float64) ([]sourceSnapshot, error) {
	r.org = org
	r.k = k
	r.minimum = minimum
	r.terms = terms
	return r.lexical, nil
}
func TestHybridServiceBoundsCandidatesAndRevalidatesPublishedSet(t *testing.T) {
	s, r, _, _, _ := fixture()
	r.rows = evidence()
	h := &hybridRepositoryFixture{testRepository: r, lexical: []sourceSnapshot{r.rows[1]}}
	s.repo = h
	got, validate, err := s.SearchWithValidation(context.Background(), 1, 2, QueryInput{Query: "AUTH_KEY_42 知识库", TopK: 1, RetrievalMode: "hybrid"})
	if err != nil || got.RetrievalMode != "hybrid" || len(got.Items) != 1 || got.Items[0].ChunkID != 12 || got.Items[0].Score != .8 {
		t.Fatalf("%+v %v", got, err)
	}
	if r.requestedK != 20 || h.k != 20 || h.org != 2 || h.minimum != .4 || !reflect.DeepEqual(h.terms, []string{"auth_key_42", "知识", "识库"}) {
		t.Fatalf("query boundary %+v", h)
	}
	r.validateErr = NewError("INDEX_SOURCE_CHANGED")
	codeIs(t, validate(context.Background()), "INDEX_SOURCE_CHANGED")
}
func TestAskPreservesMeasuredUsageAndGenerationTimeOnRefusal(t *testing.T) {
	s, r, _, _, g := fixture()
	r.rows = evidence()
	prompt, completion, total := int64(10), int64(4), int64(14)
	g.fn = func(context.Context, string, []Source) (Generated, error) {
		return Generated{InsufficientEvidence: true, Model: "deepseek-flash", Usage: Usage{Known: true, PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total}}, nil
	}
	got, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q", RetrievalMode: "hybrid"})
	if err != nil || !got.InsufficientEvidence || !got.Usage.Known || !got.Usage.ModelCalled || got.Usage.TotalTokens == nil || *got.Usage.TotalTokens != 14 || got.GenerationModel != "deepseek-flash" || got.Usage.CacheHitTokens != nil || got.Timings.TotalMS < got.Timings.GenerationMS {
		t.Fatalf("%+v %v", got, err)
	}
}

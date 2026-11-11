package rag

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
)

type testAccess struct{ err error }

func (a *testAccess) Authorize(context.Context, int64, int64) error { return a.err }
func (a *testAccess) Get(context.Context, int64, int64, int64) (knowledge.Document, error) {
	return knowledge.Document{ID: 3, OrgID: 2, Status: knowledge.Ready}, a.err
}
func (a *testAccess) Text(context.Context, int64, int64, int64) (knowledge.DocumentText, error) {
	return knowledge.DocumentText{}, a.err
}

type testEmbedder struct {
	fn    func(context.Context, []string) ([][]float32, error)
	calls int
}

func (*testEmbedder) Model() string  { return EmbeddingModel }
func (*testEmbedder) Dimension() int { return EmbeddingDimension }
func (e *testEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.fn != nil {
		return e.fn(ctx, texts)
	}
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, 1024)
		vectors[i][0] = 1
	}
	return vectors, nil
}

type testGenerator struct {
	fn    func(context.Context, string, []Source) (Generated, error)
	calls int
}

func (*testGenerator) Model() string { return ChatModel }
func (g *testGenerator) Generate(c context.Context, q string, s []Source) (Generated, error) {
	g.calls++
	if g.fn != nil {
		return g.fn(c, q, s)
	}
	return Generated{Answer: "grounded", CitationIDs: []int64{11, 11}}, nil
}

type testRepository struct {
	claim                              indexClaim
	claimErr, errorFinish, validateErr error
	rows                               []sourceSnapshot
	finished, failed, validations      int
	validateFn                         func([]sourceSnapshot) error
	failureCode                        string
	failureCtxErr                      error
	vectors                            [][]float32
	chunks                             []knowledge.Chunk
	requestedK                         int
	requestedVector                    []float32
}

func (r *testRepository) Claim(context.Context, int64, int64) (indexClaim, error) {
	return r.claim, r.claimErr
}
func (r *testRepository) Finish(_ context.Context, _ int64, _ indexClaim, c []knowledge.Chunk, v [][]float32) (IndexInfo, error) {
	r.finished++
	r.chunks = c
	r.vectors = v
	info := r.claim.Info
	info.Status = "READY"
	info.ChunkCount = len(c)
	return info, r.errorFinish
}
func (r *testRepository) Fail(ctx context.Context, _ int64, _ int64, _ int64, code string) error {
	r.failed++
	r.failureCode = code
	r.failureCtxErr = ctx.Err()
	return nil
}
func (r *testRepository) Status(context.Context, int64, int64) (IndexInfo, error) {
	return absentIndex(3), nil
}
func (r *testRepository) Search(_ context.Context, _ int64, v []float32, k int, _ float64) ([]sourceSnapshot, error) {
	r.requestedK = k
	r.requestedVector = v
	return r.rows, nil
}
func (r *testRepository) LexicalSearch(context.Context, int64, []float32, []string, int, float64) ([]sourceSnapshot, error) {
	return nil, nil
}
func (r *testRepository) Validate(_ context.Context, _ int64, sources []sourceSnapshot) error {
	r.validations++
	if r.validateFn != nil {
		return r.validateFn(sources)
	}
	return r.validateErr
}
func fixture() (*Service, *testRepository, *testAccess, *testEmbedder, *testGenerator) {
	a := &testAccess{}
	e := &testEmbedder{}
	g := &testGenerator{}
	r := &testRepository{claim: indexClaim{Info: IndexInfo{DocumentID: 3, IndexVersion: 1, Status: "INDEXING"}, Content: "original text"}}
	s := NewService(nil, a, e, g, Limits{Enabled: true, MinimumScore: .4})
	s.repo = r
	return s, r, a, e, g
}
func codeIs(t *testing.T, err error, want string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
func evidence() []sourceSnapshot {
	return []sourceSnapshot{{Source: Source{ChunkID: 11, DocumentID: 3, Excerpt: "original text", Score: .9}, Hash: "hash", Version: 1}, {Source: Source{ChunkID: 12, DocumentID: 4, Excerpt: "other evidence", Score: .8}, Hash: "hash2", Version: 2}}
}

func TestIndexReusesReadyWithoutEmbedding(t *testing.T) {
	s, r, _, e, _ := fixture()
	r.claim.Reused = true
	r.claim.Info.Status = "READY"
	r.claim.Info.ChunkCount = 2
	info, err := s.Index(context.Background(), 1, 2, 3)
	if err != nil || info.ChunkCount != 2 || e.calls != 0 || r.finished != 0 {
		t.Fatalf("reuse: %+v %v calls=%d", info, err, e.calls)
	}
}
func TestIndexBatchesAndNormalizesVectors(t *testing.T) {
	s, r, _, e, _ := fixture()
	r.claim.Content = strings.Repeat("中", 4000)
	var sizes []int
	e.fn = func(_ context.Context, texts []string) ([][]float32, error) {
		sizes = append(sizes, len(texts))
		v := make([][]float32, len(texts))
		for i := range v {
			v[i] = make([]float32, 1024)
			v[i][0] = math.MaxFloat32
			v[i][1] = math.MaxFloat32
		}
		return v, nil
	}
	info, err := s.Index(context.Background(), 1, 2, 3)
	if err != nil || info.ChunkCount != 10 || len(sizes) != 2 || sizes[0] != 8 || sizes[1] != 2 {
		t.Fatalf("batches: %+v %v %v", info, err, sizes)
	}
	if math.Abs(float64(r.vectors[0][0])-.70710678) > 1e-6 || r.chunks[1].StartRune != 420 {
		t.Fatal("vector normalization or offsets lost")
	}
}
func TestIndexRejectsInvalidEmbeddingBeforeFinish(t *testing.T) {
	for _, kind := range []string{"count", "dim", "zero", "nan"} {
		t.Run(kind, func(t *testing.T) {
			s, r, _, e, _ := fixture()
			e.fn = func(context.Context, []string) ([][]float32, error) {
				v := make([]float32, 1024)
				switch kind {
				case "count":
					return nil, nil
				case "dim":
					return [][]float32{{1}}, nil
				case "nan":
					v[0] = float32(math.NaN())
				}
				return [][]float32{v}, nil
			}
			_, err := s.Index(context.Background(), 1, 2, 3)
			if err == nil || r.finished != 0 || r.failed != 1 {
				t.Fatalf("invalid vector published: %v %+v", err, r)
			}
		})
	}
}
func TestIndexTimeoutPersistsIndependentFailure(t *testing.T) {
	s, r, _, e, _ := fixture()
	s.limits.IndexTimeout = time.Millisecond
	e.fn = func(ctx context.Context, _ []string) ([][]float32, error) { <-ctx.Done(); return nil, ctx.Err() }
	_, err := s.Index(context.Background(), 1, 2, 3)
	codeIs(t, err, "INDEX_TIMEOUT")
	if r.failureCode != "INDEX_TIMEOUT" || r.failureCtxErr != nil {
		t.Fatalf("cleanup canceled: %+v", r)
	}
}
func TestUncertainClaimDoesNotMarkOtherRunFailed(t *testing.T) {
	s, r, _, _, _ := fixture()
	r.claimErr = knowledge.ErrUncertain
	_, err := s.Index(context.Background(), 1, 2, 3)
	if err == nil || r.failed != 0 {
		t.Fatal("unknown claim must not fail a run")
	}
}
func TestIndexSourceChangePreventsPublication(t *testing.T) {
	s, r, _, _, _ := fixture()
	r.errorFinish = NewError("INDEX_SOURCE_CHANGED")
	_, err := s.Index(context.Background(), 1, 2, 3)
	codeIs(t, err, "INDEX_SOURCE_CHANGED")
	if r.failureCode != "INDEX_SOURCE_CHANGED" {
		t.Fatal("missing conditional failure")
	}
}
func TestSearchDefaultsAndMembershipRevokedDuringEmbedding(t *testing.T) {
	s, r, a, e, _ := fixture()
	e.fn = func(context.Context, []string) ([][]float32, error) {
		a.err = knowledge.ErrNotFound
		v := make([]float32, 1024)
		v[0] = 1
		return [][]float32{v}, nil
	}
	_, err := s.Search(context.Background(), 1, 2, QueryInput{Query: "q"})
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("revocation: %v", err)
	}
	a.err = nil
	e.fn = nil
	res, err := s.Search(context.Background(), 1, 2, QueryInput{Query: " q "})
	if err != nil || res.Items == nil || r.requestedK != 5 {
		t.Fatalf("defaults: %+v %v", res, err)
	}
}
func TestAskNoEvidenceSkipsGenerator(t *testing.T) {
	s, _, _, _, g := fixture()
	res, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q"})
	if err != nil || !res.InsufficientEvidence || res.Answer != "知识库未检索到足够依据。" || res.Sources == nil || g.calls != 0 {
		t.Fatalf("empty evidence: %+v %v", res, err)
	}
}
func TestAskValidatesUncitedSourceAfterGeneration(t *testing.T) {
	s, r, _, _, g := fixture()
	r.rows = evidence()
	g.fn = func(_ context.Context, _ string, sources []Source) (Generated, error) {
		if len(sources) != 2 {
			t.Fatal("lost evidence")
		}
		r.validateFn = func(rows []sourceSnapshot) error {
			for _, row := range rows {
				if row.ChunkID == 12 {
					return NewError("INDEX_SOURCE_CHANGED")
				}
			}
			return nil
		}
		return Generated{Answer: "answer", CitationIDs: []int64{11}}, nil
	}
	_, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q"})
	codeIs(t, err, "INDEX_SOURCE_CHANGED")
	if r.validations < 2 {
		t.Fatal("must validate before and after generation")
	}
}
func TestAskCitationContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		gen  Generated
		want string
	}{{"valid", Generated{Answer: " grounded ", CitationIDs: []int64{11, 11}}, ""}, {"unknown", Generated{Answer: "text", CitationIDs: []int64{999}}, "MODEL_INVALID_RESPONSE"}, {"none", Generated{Answer: "text"}, "MODEL_INVALID_RESPONSE"}, {"blank", Generated{Answer: " ", CitationIDs: []int64{11}}, "MODEL_INVALID_RESPONSE"}, {"oversize", Generated{Answer: strings.Repeat("a", 4001), CitationIDs: []int64{11}}, "MODEL_INVALID_RESPONSE"}, {"insufficient", Generated{Answer: "unsafe", InsufficientEvidence: true}, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, _, _, g := fixture()
			r.rows = evidence()
			g.fn = func(context.Context, string, []Source) (Generated, error) { return tc.gen, nil }
			res, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q"})
			if tc.want != "" {
				codeIs(t, err, tc.want)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.gen.InsufficientEvidence {
				if res.Answer != "知识库未检索到足够依据。" || len(res.Sources) != 0 {
					t.Fatal("model refusal not canonical")
				}
			} else if res.Answer != "grounded" || len(res.Sources) != 1 || res.Sources[0].ChunkID != 11 || len(res.RequestID) != 32 {
				t.Fatalf("canonical answer: %+v", res)
			}
		})
	}
}
func TestDisabledModelsStillAllowStatus(t *testing.T) {
	s, _, _, e, _ := fixture()
	s.limits.Enabled = false
	_, err := s.Index(context.Background(), 1, 2, 3)
	codeIs(t, err, "MODEL_UNAVAILABLE")
	_, err = s.Search(context.Background(), 1, 2, QueryInput{Query: "q"})
	codeIs(t, err, "MODEL_UNAVAILABLE")
	info, err := s.IndexStatus(context.Background(), 1, 2, 3)
	if err != nil || info.Status != "ABSENT" || e.calls != 0 {
		t.Fatal("disabled status unavailable")
	}
}

func TestIndexUnexpectedAdapterErrorUsesAllowedFailureCode(t *testing.T) {
	s, r, _, e, _ := fixture()
	e.fn = func(context.Context, []string) ([][]float32, error) { return nil, NewError("INVALID_REQUEST") }
	_, err := s.Index(context.Background(), 1, 2, 3)
	codeIs(t, err, "INVALID_REQUEST")
	if r.failureCode != "INDEX_FAILED" {
		t.Fatalf("failure code violates database allowlist: %s", r.failureCode)
	}
}
func TestIndexRevocationDuringEmbeddingPreventsFinish(t *testing.T) {
	s, r, a, e, _ := fixture()
	e.fn = func(context.Context, []string) ([][]float32, error) {
		a.err = knowledge.ErrNotFound
		v := make([]float32, 1024)
		v[0] = 1
		return [][]float32{v}, nil
	}
	_, err := s.Index(context.Background(), 1, 2, 3)
	if !errors.Is(err, knowledge.ErrNotFound) || r.finished != 0 || r.failed != 1 {
		t.Fatalf("revocation published index: %v", err)
	}
}
func TestAskRevokedMemberNeverReceivesGeneratedAnswer(t *testing.T) {
	s, r, a, _, g := fixture()
	r.rows = evidence()
	g.fn = func(context.Context, string, []Source) (Generated, error) {
		a.err = knowledge.ErrNotFound
		return Generated{Answer: "private answer", CitationIDs: []int64{11}}, nil
	}
	res, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "question"})
	if !errors.Is(err, knowledge.ErrNotFound) || res.Answer != "" {
		t.Fatalf("revoked answer: %+v %v", res, err)
	}
}
func TestAskTimeoutDiscardsLateSuccessfulGeneration(t *testing.T) {
	s, r, _, _, g := fixture()
	r.rows = evidence()
	s.limits.AskTimeout = time.Millisecond
	g.fn = func(ctx context.Context, _ string, _ []Source) (Generated, error) {
		<-ctx.Done()
		return Generated{Answer: "late answer", CitationIDs: []int64{11}}, nil
	}
	res, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q"})
	codeIs(t, err, "MODEL_TIMEOUT")
	if res.Answer != "" {
		t.Fatal("late model output leaked")
	}
}
func TestSearchRejectsChangedSourceAndInvalidEmbedding(t *testing.T) {
	s, r, _, e, _ := fixture()
	r.rows = evidence()
	r.validateErr = NewError("INDEX_SOURCE_CHANGED")
	_, err := s.Search(context.Background(), 1, 2, QueryInput{Query: "q"})
	codeIs(t, err, "INDEX_SOURCE_CHANGED")
	e.fn = func(context.Context, []string) ([][]float32, error) { return [][]float32{make([]float32, 1024)}, nil }
	_, err = s.Search(context.Background(), 1, 2, QueryInput{Query: "q"})
	codeIs(t, err, "EMBEDDING_INVALID_VECTOR")
}

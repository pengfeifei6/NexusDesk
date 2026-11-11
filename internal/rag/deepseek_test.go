package rag

import (
	"context"
	"testing"
)

type cloudGenerator struct {
	testGenerator
	model string
}

func (g *cloudGenerator) Model() string { return g.model }
func TestAskCloudGenerationModelAndCitationValidation(t *testing.T) {
	for _, model := range []string{"deepseek-flash", "deepseek-v4-pro"} {
		t.Run(model, func(t *testing.T) {
			s, r, _, _, _ := fixture()
			r.rows = evidence()
			g := &cloudGenerator{model: model}
			s.generator = g
			result, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "question"})
			if err != nil || result.GenerationModel != model || len(result.Sources) != 1 || result.Sources[0].ChunkID != 11 {
				t.Fatalf("cloud ask failed: %+v %v", result, err)
			}
			g.fn = func(context.Context, string, []Source) (Generated, error) {
				return Generated{Answer: "invented", CitationIDs: []int64{999}}, nil
			}
			_, err = s.Ask(context.Background(), 1, 2, QueryInput{Query: "question"})
			codeIs(t, err, "MODEL_INVALID_RESPONSE")
		})
	}
	s, _, _, _, _ := fixture()
	s.generator = &cloudGenerator{model: "unconfigured-model"}
	_, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "question"})
	codeIs(t, err, "MODEL_UNAVAILABLE")
}

package rag

import (
	"context"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
	"strings"
	"testing"
)

func TestAskCorrelatesWithHTTPIdentityAndGeneratesStandaloneIdentity(t *testing.T) {
	s, _, _, _, _ := fixture()
	ctx := observe.WithRequestID(context.Background())
	res, err := s.Ask(ctx, 1, 2, QueryInput{Query: "q"})
	if err != nil || res.RequestID != observe.RequestID(ctx) {
		t.Fatal(res, err)
	}
	standalone, err := s.Ask(context.Background(), 1, 2, QueryInput{Query: "q"})
	if err != nil || len(standalone.RequestID) != 32 || standalone.RequestID == res.RequestID || strings.Trim(standalone.RequestID, "0123456789abcdef") != "" {
		t.Fatal(standalone, err)
	}
}

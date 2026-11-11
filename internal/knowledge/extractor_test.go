package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTextExtractionNormalizesAndBounds(t *testing.T) {
	x := NewTextExtractor(1000, 100)
	got, err := x.Extract(context.Background(), strings.NewReader("\ufeff# 标题\r\n第一段\r第二段\n"), "text/markdown")
	if err != nil || got.ContentText != "# 标题\n第一段\n第二段\n" || got.Chars != int64(utf8.RuneCountInString(got.ContentText)) || len(got.ContentSHA256) != 64 || got.ExtractorVersion != "utf8-v1" {
		t.Fatal("normalization", err)
	}
	for _, tc := range []struct{ name, body, mime, code string }{
		{"whitespace", " \t\r\n", "text/plain", "EMPTY_TEXT"},
		{"invalid_utf8", string([]byte{0xff}), "text/plain", "INVALID_TEXT"},
		{"nul", "a\x00b", "text/plain", "INVALID_TEXT"},
		{"too_big", strings.Repeat("中", 34), "text/plain", "TEXT_TOO_LARGE"},
		{"pdf", "%PDF-1.7\n", "application/pdf", "PDF_UNSUPPORTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := x.Extract(context.Background(), strings.NewReader(tc.body), tc.mime)
			var failure *ExtractionError
			if !errors.As(err, &failure) || failure.Code != tc.code {
				t.Fatalf("got %v want %s", err, tc.code)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := x.Extract(ctx, strings.NewReader("hello"), "text/plain"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}

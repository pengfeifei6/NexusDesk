package knowledge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"unicode/utf8"
)

type ExtractedText struct {
	ContentText      string
	ContentSHA256    string
	ExtractorVersion string
	Chars            int64
}
type ExtractionError struct{ Code string }

func (e *ExtractionError) Error() string { return e.Code }

type TextExtractor interface {
	Extract(context.Context, io.Reader, string) (ExtractedText, error)
}
type UTF8Extractor struct{ maxSource, maxText int64 }

func NewTextExtractor(maxSource, maxText int64) *UTF8Extractor {
	return &UTF8Extractor{maxSource, maxText}
}
func (x *UTF8Extractor) Extract(ctx context.Context, source io.Reader, mediaType string) (ExtractedText, error) {
	if err := ctx.Err(); err != nil {
		return ExtractedText{}, err
	}
	if mediaType == "application/pdf" {
		return ExtractedText{}, &ExtractionError{"PDF_UNSUPPORTED"}
	}
	if mediaType != "text/plain" && mediaType != "text/markdown" {
		return ExtractedText{}, &ExtractionError{"INVALID_TEXT"}
	}
	reader := bufio.NewReader(io.LimitReader(source, x.maxSource+1))
	var out strings.Builder
	var bytesRead int64
	first, previousCR := true, false
	for {
		if err := ctx.Err(); err != nil {
			return ExtractedText{}, err
		}
		r, n, err := reader.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ExtractedText{}, err
		}
		bytesRead += int64(n)
		if bytesRead > x.maxSource {
			return ExtractedText{}, &ExtractionError{"TEXT_TOO_LARGE"}
		}
		if r == utf8.RuneError && n == 1 || r == 0 || r < 32 && r != '\n' && r != '\r' && r != '\t' {
			return ExtractedText{}, &ExtractionError{"INVALID_TEXT"}
		}
		if first {
			first = false
			if r == '\ufeff' {
				continue
			}
		}
		if previousCR && r == '\n' {
			previousCR = false
			continue
		}
		previousCR = r == '\r'
		if previousCR {
			r = '\n'
		}
		if int64(out.Len()+utf8.RuneLen(r)) > x.maxText {
			return ExtractedText{}, &ExtractionError{"TEXT_TOO_LARGE"}
		}
		out.WriteRune(r)
	}
	if err := ctx.Err(); err != nil {
		return ExtractedText{}, err
	}
	content := out.String()
	if strings.TrimSpace(content) == "" {
		return ExtractedText{}, &ExtractionError{"EMPTY_TEXT"}
	}
	sum := sha256.Sum256([]byte(content))
	return ExtractedText{content, hex.EncodeToString(sum[:]), "utf8-v1", int64(utf8.RuneCountInString(content))}, nil
}

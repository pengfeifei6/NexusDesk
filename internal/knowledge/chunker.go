package knowledge

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const ChunkerVersion = "rune-500-80-v1"
const ChunkWindow = 500
const ChunkOverlap = 80
const MaxIndexChunks = 200

var ErrChunkInvalid = errors.New("document text cannot be indexed")
var ErrChunkTooMany = errors.New("document exceeds chunk limit")

type Chunk struct {
	Index, StartRune, EndRune int
	Content                   string
}

// SplitText preserves offsets in the stored extracted text, not the original file.
func SplitText(text string) ([]Chunk, error) {
	if len(text) > 1048576 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" {
		return nil, ErrChunkInvalid
	}
	runes := []rune(text)
	chunks := make([]Chunk, 0, min(MaxIndexChunks, 1+len(runes)/(ChunkWindow-ChunkOverlap)))
	for start := 0; start < len(runes); start += ChunkWindow - ChunkOverlap {
		end := min(start+ChunkWindow, len(runes))
		content := string(runes[start:end])
		if strings.TrimSpace(content) != "" {
			if len(chunks) == MaxIndexChunks {
				return nil, ErrChunkTooMany
			}
			chunks = append(chunks, Chunk{Index: len(chunks), StartRune: start, EndRune: end, Content: content})
		}
		if end == len(runes) {
			break
		}
	}
	return chunks, nil
}

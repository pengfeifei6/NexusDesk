package rag

import (
	"sort"
	"strings"
	"unicode"
)

// queryTerms keeps literal ASCII identifiers and overlapping Han bigrams.
// Terms are deduplicated in first-seen order and never interpreted as SQL patterns.
func queryTerms(query string) []string {
	terms := make([]string, 0, 32)
	seen := map[string]bool{}
	add := func(term string) {
		if !seen[term] && len(terms) < 32 {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	runes := []rune(strings.ToLower(query))
	for i := 0; i < len(runes) && len(terms) < 32; {
		r := runes[i]
		if asciiTerm(r) {
			j := i + 1
			for j < len(runes) && asciiTerm(runes[j]) {
				j++
			}
			add(string(runes[i:j]))
			i = j
		} else if unicode.Is(unicode.Han, r) {
			j := i + 1
			for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			for n := i; n+1 < j; n++ {
				add(string(runes[n : n+2]))
			}
			i = j
		} else {
			i++
		}
	}
	return terms
}
func asciiTerm(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' }

// fuseSources uses reciprocal rank fusion; Score stays the original cosine.
func fuseSources(vector, lexical []sourceSnapshot, k int) []sourceSnapshot {
	items := map[int64]sourceSnapshot{}
	scores := map[int64]float64{}
	for _, list := range [][]sourceSnapshot{vector, lexical} {
		seen := map[int64]bool{}
		for rank, item := range list {
			if seen[item.ChunkID] {
				continue
			}
			seen[item.ChunkID] = true
			if _, exists := items[item.ChunkID]; !exists {
				items[item.ChunkID] = item
			}
			scores[item.ChunkID] += 1 / float64(60+rank+1)
		}
	}
	result := make([]sourceSnapshot, 0, len(items))
	for id, item := range items {
		score := scores[id]
		item.FusionScore = &score
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if *a.FusionScore == *b.FusionScore {
			return a.ChunkID < b.ChunkID
		}
		return *a.FusionScore > *b.FusionScore
	})
	if len(result) > k {
		result = result[:k]
	}
	return result
}

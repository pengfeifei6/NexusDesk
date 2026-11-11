package knowledge

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkC01EmptyOrInvalid(t *testing.T) {
	for _, text := range []string{"", " \t\n\r", string([]byte{0xff}), "a\x00b", strings.Repeat("x", 1048577)} {
		chunks, err := SplitText(text)
		if !errors.Is(err, ErrChunkInvalid) || len(chunks) != 0 {
			t.Fatal("invalid source accepted")
		}
	}
}
func TestChunkC02C03C04C05C06Offsets(t *testing.T) {
	for _, text := range []string{"short", strings.Repeat("x", 500), strings.Repeat("x", 501), strings.Repeat("中🧪文a", 310), strings.Repeat("第一段\n\n第二段\r\n", 100), strings.Repeat("a", 499) + strings.Repeat(" ", 700) + "z"} {
		t.Run(string([]rune(text)[:1]), func(t *testing.T) {
			chunks, err := SplitText(text)
			if err != nil || len(chunks) == 0 {
				t.Fatal(err)
			}
			runes := []rune(text)
			covered := make([]bool, len(runes))
			for i, c := range chunks {
				if c.Index != i || c.StartRune < 0 || c.EndRune > len(runes) || c.EndRune <= c.StartRune || c.EndRune-c.StartRune > 500 {
					t.Fatal("invalid chunk order or span")
				}
				if c.Content != string(runes[c.StartRune:c.EndRune]) || !utf8.ValidString(c.Content) {
					t.Fatal("offset not reversible")
				}
				if i > 0 && c.StartRune <= chunks[i-1].StartRune {
					t.Fatal("nonprogressing window")
				}
				for j := c.StartRune; j < c.EndRune; j++ {
					covered[j] = true
				}
			}
			for i, r := range runes {
				if !covered[i] && strings.TrimSpace(string(r)) != "" {
					t.Fatal("source character lost")
				}
			}
			if len(runes) <= 500 && len(chunks) != 1 {
				t.Fatal("extra empty chunk")
			}
			if len(runes) == 501 && (len(chunks) != 2 || chunks[1].StartRune != 420) {
				t.Fatal("incorrect overlap")
			}
		})
	}
}
func TestChunkC07NoPartialSuccessOnLimit(t *testing.T) {
	chunks, err := SplitText(strings.Repeat("中", 84080))
	if err != nil || len(chunks) != 200 {
		t.Fatal("exact chunk limit", err)
	}
	chunks, err = SplitText(strings.Repeat("中", 84081))
	if !errors.Is(err, ErrChunkTooMany) || len(chunks) != 0 {
		t.Fatal("over limit returned partial success")
	}
}
func TestChunkC08Deterministic(t *testing.T) {
	text := strings.Repeat("重复输入🧪\n", 201)
	first, err := SplitText(text)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		next, err := SplitText(text)
		if err != nil || !reflect.DeepEqual(first, next) {
			t.Fatal("nondeterministic chunks")
		}
	}
}

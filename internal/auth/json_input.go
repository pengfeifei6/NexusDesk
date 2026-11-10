package auth

import (
	"strconv"
	"unicode/utf8"
)

// validJSONUnicode rejects text encoding that encoding/json would otherwise
// replace with U+FFFD. The normal decoder still validates the JSON grammar.
func validJSONUnicode(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	quoted := false
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '"':
			quoted = !quoted
		case '\\':
			if !quoted {
				continue
			}
			if i+1 >= len(body) {
				return false
			}
			if body[i+1] != 'u' {
				i++ // Skip an escaped quote or backslash without changing string state.
				continue
			}
			unit, ok := unicodeEscape(body, i)
			if !ok || (unit >= 0xdc00 && unit <= 0xdfff) {
				return false
			}
			i += 5
			if unit >= 0xd800 && unit <= 0xdbff {
				low, ok := unicodeEscape(body, i+1)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

func unicodeEscape(body []byte, offset int) (uint16, bool) {
	if offset+6 > len(body) || body[offset] != '\\' || body[offset+1] != 'u' {
		return 0, false
	}
	value, err := strconv.ParseUint(string(body[offset+2:offset+6]), 16, 16)
	return uint16(value), err == nil
}

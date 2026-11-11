package knowledge

import (
	"bufio"
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func uploadReader(name string, source io.Reader) (string, io.Reader, error) {
	if name == "" || len(name) > 200 || !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return "", nil, ErrInvalidRequest
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", nil, ErrInvalidRequest
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	if len(name) == len(ext) {
		return "", nil, ErrInvalidRequest
	}
	media := map[string]string{".txt": "text/plain", ".md": "text/markdown", ".pdf": "application/pdf"}[ext]
	if media == "" {
		return "", nil, ErrUnsupported
	}
	reader := bufio.NewReader(source)
	header, err := reader.Peek(8)
	if err != nil && err != io.EOF {
		return "", nil, err
	}
	if len(header) == 0 {
		return "", nil, ErrInvalidRequest
	}
	if media == "application/pdf" {
		if len(header) < 8 || !bytes.HasPrefix(header, []byte("%PDF-")) || (header[5] != '1' && header[5] != '2') || header[6] != '.' || header[7] < '0' || header[7] > '9' {
			return "", nil, ErrUnsupported
		}
		return media, reader, nil
	}
	for _, signature := range [][]byte{[]byte("%PDF-"), {'P', 'K', 3, 4}, {0x7f, 'E', 'L', 'F'}, {0x89, 'P', 'N', 'G'}} {
		if bytes.HasPrefix(header, signature) {
			return "", nil, ErrUnsupported
		}
	}
	return media, &validatingTextReader{r: reader}, nil
}

// Keep only an incomplete UTF-8 rune across chunks; never retain the whole upload.
type validatingTextReader struct {
	r       io.Reader
	pending []byte
}

func (v *validatingTextReader) Read(p []byte) (int, error) {
	n, err := v.r.Read(p)
	data := append(v.pending, p[:n]...)
	v.pending = nil
	for len(data) > 0 {
		if !utf8.FullRune(data) && err == nil {
			v.pending = append([]byte(nil), data...)
			break
		}
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 || unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return 0, ErrUnsupported
		}
		data = data[size:]
	}
	return n, err
}

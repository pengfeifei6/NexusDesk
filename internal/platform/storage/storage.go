// Package storage provides bounded access to private, server-named objects.
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

var (
	ErrInvalidKey = errors.New("invalid object key")
	ErrTooLarge   = errors.New("object exceeds size limit")
	ErrEmpty      = errors.New("empty object")
	ErrExists     = errors.New("object key exists")
	ErrIO         = errors.New("private object storage unavailable")
)

type Object struct {
	SizeBytes int64
	SHA256Hex string
}
type ObjectStorage interface {
	Put(context.Context, string, io.Reader, int64) (Object, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
	RecordCleanup(context.Context, string, string) error
}

func NewKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrIO
	}
	return hex.EncodeToString(b[:]), nil
}
func validKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, c := range key {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

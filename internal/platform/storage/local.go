package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Local struct{ root *os.Root }

func NewLocal(directory string) (*Local, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, ErrIO
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, ErrIO
	}
	for _, public := range []string{filepath.Join(cwd, "web"), filepath.Join(cwd, ".git")} {
		rel, err := filepath.Rel(public, absolute)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, ErrIO
		}
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, ErrIO
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrIO
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, ErrIO
	}
	for _, public := range []string{filepath.Join(cwd, "web"), filepath.Join(cwd, ".git")} {
		if canonical, e := filepath.EvalSymlinks(public); e == nil {
			public = canonical
		}
		rel, e := filepath.Rel(public, resolved)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, ErrIO
		}
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, ErrIO
	}
	return &Local{root: root}, nil
}
func (s *Local) Close() error { return s.root.Close() }

func (s *Local) Put(ctx context.Context, key string, r io.Reader, maxBytes int64) (Object, error) {
	if !validKey(key) {
		return Object{}, ErrInvalidKey
	}
	if maxBytes <= 0 || maxBytes > 64<<20 || r == nil {
		return Object{}, ErrIO
	}
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	suffix, err := NewKey()
	if err != nil {
		return Object{}, err
	}
	temp := ".upload-" + suffix
	f, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Object{}, ErrIO
	}
	defer f.Close()
	defer s.cleanupTemporary(temp)
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(contextReader{ctx, r}, maxBytes+1))
	if n > maxBytes {
		return Object{}, ErrTooLarge
	}
	if err != nil {
		return Object{}, err
	}
	if n == 0 {
		return Object{}, ErrEmpty
	}
	if err = ctx.Err(); err != nil {
		return Object{}, err
	}
	if err = f.Sync(); err != nil {
		return Object{}, ErrIO
	}
	if err = f.Close(); err != nil {
		return Object{}, ErrIO
	}
	// Same filesystem publication with no overwrite, including symlink collisions.
	if err = s.root.Link(temp, key); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Object{}, ErrExists
		}
		return Object{}, ErrIO
	}
	if err = s.syncRoot(); err != nil {
		if cleanup := s.Delete(context.Background(), key); cleanup != nil {
			_ = s.RecordCleanup(context.Background(), key, "delete_failed")
		}
		return Object{}, ErrIO
	}
	return Object{SizeBytes: n, SHA256Hex: hex.EncodeToString(hash.Sum(nil))}, nil
}
func (s *Local) syncRoot() error {
	dir, err := s.root.Open(".")
	if err != nil {
		return ErrIO
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return ErrIO
	}
	return nil
}
func (s *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := s.root.Lstat(key)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrIO
	}
	f, err := s.root.Open(key)
	if err != nil {
		return nil, ErrIO
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(info, after) {
		f.Close()
		return nil, ErrIO
	}
	return f, nil
}
func (s *Local) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrIO
	}
	return s.syncRoot()
}
func (s *Local) RecordCleanup(ctx context.Context, key, reason string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	if reason != "delete_failed" && reason != "metadata_outcome_unknown" {
		return ErrIO
	}
	return s.recordCleanup(ctx, key, reason)
}
func (s *Local) cleanupTemporary(key string) {
	err := s.root.Remove(key)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		err = s.syncRoot()
	}
	if err == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.recordCleanup(ctx, key, "temporary_delete_failed") != nil {
		slog.Error("temporary object cleanup and journal failed", "temporary_key", key)
	}
}
func (s *Local) recordCleanup(ctx context.Context, key, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Key       string    `json:"key"`
		Reason    string    `json:"reason"`
		CreatedAt time.Time `json:"created_at"`
	}{key, reason, time.Now().UTC()})
	if err != nil {
		return ErrIO
	}
	// Publish only complete synced JSON. Rename replaces a corrupt record or
	// symlink itself, never the symlink target; the directory is service-private.
	suffix, err := NewKey()
	if err != nil {
		return err
	}
	temp := ".journal-" + suffix
	f, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrIO
	}
	defer f.Close()
	defer s.root.Remove(temp)
	if _, err = f.Write(append(data, '\n')); err != nil {
		return ErrIO
	}
	if err = f.Sync(); err != nil {
		return ErrIO
	}
	if err = f.Close(); err != nil {
		return ErrIO
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.root.Rename(temp, key+".cleanup.json"); err != nil {
		return ErrIO
	}
	return s.syncRoot()
}

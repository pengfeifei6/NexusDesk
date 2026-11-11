package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStreamsHashAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key := strings.Repeat("a", 64)
	body := "# 原创测试\nThis is a fixture.\n"
	got, err := s.Put(context.Background(), key, strings.NewReader(body), 1000)
	sum := sha256.Sum256([]byte(body))
	if err != nil || got.SizeBytes != int64(len(body)) || got.SHA256Hex != hex.EncodeToString(sum[:]) {
		t.Fatal("object metadata mismatch", err)
	}
	if _, err = s.Put(context.Background(), key, strings.NewReader("replacement"), 1000); !errors.Is(err, ErrExists) {
		t.Fatal("must refuse collision", err)
	}
	r, err := s.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != body {
		t.Fatal("original changed")
	}
	info, err := os.Stat(filepath.Join(dir, key))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("object permissions")
	}
	if err = s.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), key); err != nil {
		t.Fatal("cleanup delete must be idempotent")
	}
}

func TestLocalLimitsCancellationAndReaderFailureLeaveNoFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int
		limit    int64
		canceled bool
		want     error
	}{
		{"exact", 32, 32, false, nil}, {"too_large", 33, 32, false, ErrTooLarge}, {"empty", 0, 32, false, ErrEmpty}, {"canceled", 20, 32, true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			os.Chmod(dir, 0700)
			s, err := NewLocal(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			_, err = s.Put(ctx, strings.Repeat("b", 64), strings.NewReader(strings.Repeat("x", tc.size)), tc.limit)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			entries, _ := os.ReadDir(dir)
			want := 0
			if tc.want == nil {
				want = 1
			}
			if len(entries) != want {
				t.Fatalf("files left: %d", len(entries))
			}
		})
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, _ := NewLocal(dir)
	defer s.Close()
	marker := errors.New("reader fixture failure")
	_, err := s.Put(context.Background(), strings.Repeat("c", 64), io.MultiReader(strings.NewReader("valid prefix"), failingReader{marker}), 100)
	if !errors.Is(err, marker) {
		t.Fatal("reader error not preserved")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("partial file remains")
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestLocalRejectsTraversalSymlinksAndRecordsCleanup(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, key := range []string{"../x", "/tmp/x", "a/b", "a\\b", "", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if _, err = s.Put(context.Background(), key, strings.NewReader("data"), 100); !errors.Is(err, ErrInvalidKey) {
			t.Fatal("unsafe write key")
		}
		if _, err = s.Open(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Fatal("unsafe read key")
		}
		if err = s.Delete(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Fatal("unsafe delete key")
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("do not expose"), 0600)
	key := strings.Repeat("d", 64)
	if err = os.Symlink(outside, filepath.Join(dir, key)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(context.Background(), key); err == nil {
		t.Fatal("followed symlink")
	}
	if _, err = s.Put(context.Background(), key, strings.NewReader("overwrite"), 100); !errors.Is(err, ErrExists) {
		t.Fatal("overwrote symlink")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "do not expose" {
		t.Fatal("outside changed")
	}
	if err = s.RecordCleanup(context.Background(), key, "delete_failed"); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(dir, key+".cleanup.json"))
	if err != nil || !strings.Contains(string(journal), "delete_failed") {
		t.Fatal("missing durable cleanup record")
	}
	if strings.Contains(string(journal), dir) {
		t.Fatal("journal contains absolute path")
	}
}

func TestLocalRejectsPublicOrInsecureRoot(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0755)
	if _, err := NewLocal(dir); err == nil {
		t.Fatal("insecure root allowed")
	}
}

func TestCleanupJournalRepairsInterruptedOrInvalidRecord(t *testing.T) {
	for _, previous := range []string{"", "{", `{"key":"wrong","reason":"delete_failed"}`, "symlink"} {
		t.Run(previous, func(t *testing.T) {
			dir := t.TempDir()
			os.Chmod(dir, 0700)
			s, err := NewLocal(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			key := strings.Repeat("e", 64)
			path := filepath.Join(dir, key+".cleanup.json")
			outside := filepath.Join(t.TempDir(), "untouched")
			if previous == "symlink" {
				os.WriteFile(outside, []byte("keep"), 0600)
				err = os.Symlink(outside, path)
			} else {
				err = os.WriteFile(path, []byte(previous), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.RecordCleanup(context.Background(), key, "metadata_outcome_unknown"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			var record struct{ Key, Reason string }
			if err != nil || json.Unmarshal(data, &record) != nil || record.Key != key || record.Reason != "metadata_outcome_unknown" {
				t.Fatal("incomplete or incorrect journal accepted")
			}
			if previous == "symlink" {
				data, _ = os.ReadFile(outside)
				if string(data) != "keep" {
					t.Fatal("journal followed outside link")
				}
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("journal temporary file leaked")
			}
		})
	}
}

type temporaryReplacementReader struct {
	t        *testing.T
	dir      string
	replaced bool
	name     string
}

func (r *temporaryReplacementReader) Read(p []byte) (int, error) {
	if r.replaced {
		return 0, io.EOF
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".upload-") {
			r.name = entry.Name()
			path := filepath.Join(r.dir, r.name)
			if err := os.Remove(path); err != nil {
				r.t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				r.t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "blocking-fixture"), []byte("fixture"), 0600); err != nil {
				r.t.Fatal(err)
			}
		}
	}
	r.replaced = true
	return copy(p, "body"), nil
}
func TestTemporaryRemovalFailureIsJournaled(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reader := &temporaryReplacementReader{t: t, dir: dir}
	if _, err = s.Put(context.Background(), strings.Repeat("f", 64), reader, 100); err == nil {
		t.Fatal("expected publish failure")
	}
	data, err := os.ReadFile(filepath.Join(dir, reader.name+".cleanup.json"))
	var record struct{ Key, Reason string }
	if err != nil || json.Unmarshal(data, &record) != nil || record.Key != reader.name || record.Reason != "temporary_delete_failed" {
		t.Fatal("temporary removal failure lost")
	}
}
func TestJournalPublicationFailureCanBeRetried(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key := strings.Repeat("f", 64)
	target := filepath.Join(dir, key+".cleanup.json")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCleanup(context.Background(), key, "metadata_outcome_unknown"); err == nil {
		t.Fatal("failed rename reported success")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("journal temporary file leaked")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCleanup(context.Background(), key, "metadata_outcome_unknown"); err != nil {
		t.Fatal(err)
	}
}
func TestLocalRejectsRootWithinPublicDirectoryViaAncestorSymlink(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	if err := os.Mkdir("web", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("web/private", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "web"), "alias"); err != nil {
		t.Fatal(err)
	}
	s, err := NewLocal("alias/private")
	if err == nil {
		s.Close()
		t.Fatal("public root accepted through ancestor symlink")
	}
}

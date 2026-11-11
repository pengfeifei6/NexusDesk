package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/storage"
	"github.com/pengfeifei6/NexusDesk/internal/platform/taskguard"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type memoryStore struct {
	doc                     Document
	text                    DocumentText
	createErr, completeErr  error
	failLive                bool
	failGuard               taskguard.Guard
	creates                 int
	claimWait, completeWait bool
}

func (m *memoryStore) Create(_ context.Context, d Document) (Document, error) {
	m.creates++
	if m.createErr != nil {
		return Document{}, m.createErr
	}
	d.ID = 1
	m.doc = d
	return d, nil
}
func (m *memoryStore) Get(_ context.Context, org, id int64) (Document, error) {
	if m.doc.ID != id || m.doc.OrgID != org || m.doc.DeletedAt != nil {
		return Document{}, ErrNotFound
	}
	return m.doc, nil
}
func (m *memoryStore) List(context.Context, int64, Filter) ([]Document, error) {
	return []Document{m.doc}, nil
}
func (m *memoryStore) ClaimProcess(ctx context.Context, org, id int64) (Document, error) {
	if m.claimWait {
		<-ctx.Done()
		return Document{}, ErrStorage
	}
	d, e := m.Get(ctx, org, id)
	if e != nil {
		return d, e
	}
	if d.Status != Uploaded && d.Status != Failed {
		return Document{}, ErrConflict
	}
	m.doc.Status = Processing
	m.doc.FailureCode = nil
	return m.doc, nil
}
func (m *memoryStore) CompleteProcess(ctx context.Context, d Document, x ExtractedText) (Document, error) {
	if m.completeWait {
		<-ctx.Done()
		return Document{}, ErrStorage
	}
	if m.completeErr != nil {
		return Document{}, m.completeErr
	}
	if _, e := m.Get(ctx, d.OrgID, d.ID); e != nil {
		return Document{}, e
	}
	m.doc.Status = Ready
	m.doc.ExtractedChars = x.Chars
	m.text = DocumentText{DocumentID: d.ID, ContentText: x.ContentText}
	return m.doc, nil
}
func (m *memoryStore) FailProcess(ctx context.Context, org, id int64, code string) error {
	m.failLive = ctx.Err() == nil
	m.failGuard, _ = taskguard.From(ctx)
	if m.doc.DeletedAt == nil && m.doc.Status == Processing {
		m.doc.Status = Failed
		m.doc.FailureCode = &code
	}
	return nil
}
func (m *memoryStore) Delete(ctx context.Context, org, id int64) error {
	if _, err := m.Get(ctx, org, id); err != nil {
		return err
	}
	now := time.Now()
	m.doc.DeletedAt = &now
	return nil
}
func (m *memoryStore) GetText(ctx context.Context, org, id int64) (DocumentText, error) {
	d, e := m.Get(ctx, org, id)
	if e != nil {
		return DocumentText{}, e
	}
	if d.Status != Ready {
		return DocumentText{}, ErrConflict
	}
	return m.text, nil
}

type memberFixture struct {
	denied bool
	role   organization.Role
}

func (m memberFixture) FindActiveMembership(_ context.Context, org, actor int64) (organization.Membership, error) {
	if m.denied {
		return organization.Membership{}, organization.ErrNotFound
	}
	role := m.role
	if role == "" {
		role = organization.RoleMember
	}
	return organization.Membership{OrgID: org, UserID: actor, Role: role, Status: organization.StatusActive}, nil
}

type brokenCleanup struct {
	storage.ObjectStorage
	recorded bool
	deletes  int
}

func (b *brokenCleanup) Delete(context.Context, string) error { b.deletes++; return storage.ErrIO }
func (b *brokenCleanup) RecordCleanup(context.Context, string, string) error {
	b.recorded = true
	return nil
}

type extractFunc func(context.Context, io.Reader, string) (ExtractedText, error)

func (f extractFunc) Extract(c context.Context, r io.Reader, m string) (ExtractedText, error) {
	return f(c, r, m)
}
func fixture(t *testing.T) (*Service, *memoryStore, *storage.Local, string) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	local, e := storage.NewLocal(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { local.Close() })
	repo := &memoryStore{}
	return NewService(repo, memberFixture{}, local, NewTextExtractor(5242880, 1048576), Limits{5242880, 8388608, 10 * time.Second}), repo, local, dir
}

func TestUploadValidatesStreamsAndCompensates(t *testing.T) {
	s, repo, local, dir := fixture(t)
	d, err := s.Upload(context.Background(), 10, 20, "guide.md", strings.NewReader("# 示例\n正文\n"))
	if err != nil || d.Status != Uploaded || d.UploadedBy != 10 || d.OrgID != 20 {
		t.Fatal("upload", err)
	}
	wire, _ := json.Marshal(d)
	if strings.Contains(string(wire), d.StorageKey) || strings.Contains(string(wire), dir) {
		t.Fatal("storage details exposed")
	}
	repo.createErr = ErrStorage
	_, err = s.Upload(context.Background(), 10, 20, "other.md", strings.NewReader("second body"))
	if err == nil {
		t.Fatal("expected DB failure")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("failed upload left file")
	}
	broken := &brokenCleanup{ObjectStorage: local}
	s.objects = broken
	_, err = s.Upload(context.Background(), 10, 20, "other.md", strings.NewReader("second body"))
	if err == nil || !broken.recorded || broken.deletes != 1 {
		t.Fatal("cleanup failure not recorded")
	}
	broken.recorded = false
	broken.deletes = 0
	repo.createErr = ErrUncertain
	_, err = s.Upload(context.Background(), 10, 20, "unknown.md", strings.NewReader("may have committed"))
	if err == nil || !broken.recorded || broken.deletes != 0 {
		t.Fatal("ambiguous commit must retain object and journal")
	}
}
func TestUploadRejectsNamesContentAndUnauthorizedBeforeIO(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{{"../../x.md", "hi", ErrInvalidRequest}, {"x\\a.md", "hi", ErrInvalidRequest}, {"x.html", "hi", ErrUnsupported}, {"x.md", "a\x00b", ErrUnsupported}, {"x.md", string([]byte{0xff}), ErrUnsupported}, {"x.md", "%PDF-1.7\nfake", ErrUnsupported}, {"x.pdf", "plain", ErrUnsupported}, {"x.txt", "", ErrInvalidRequest}} {
		s, repo, _, dir := fixture(t)
		_, err := s.Upload(context.Background(), 10, 20, tc.name, strings.NewReader(tc.body))
		if !errors.Is(err, tc.want) {
			t.Fatalf("%q got %v want %v", tc.name, err, tc.want)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 || repo.creates != 0 {
			t.Fatal("invalid upload persisted")
		}
	}
	s, repo, _, dir := fixture(t)
	s.members = memberFixture{denied: true}
	_, err := s.Upload(context.Background(), 10, 20, "valid.md", panicReader{})
	if !errors.Is(err, ErrNotFound) || repo.creates != 0 {
		t.Fatal("unauthorized IO")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("unauthorized file")
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("unauthorized reader consumed") }
func TestProcessAndDeleteStateAndFailureCleanup(t *testing.T) {
	s, repo, _, _ := fixture(t)
	ctx := context.Background()
	d, e := s.Upload(ctx, 10, 20, "x.md", strings.NewReader("# 内容\r\nhello"))
	if e != nil {
		t.Fatal(e)
	}
	ready, e := s.Process(ctx, 11, 20, d.ID)
	if e != nil || ready.Status != Ready || repo.text.ContentText != "# 内容\nhello" {
		t.Fatal("process", e)
	}
	if _, e = s.Process(ctx, 10, 20, d.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("READY reprocessed")
	}
	if e = s.Delete(ctx, 11, 20, d.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("other member deleted")
	}
	if e = s.Delete(ctx, 10, 20, d.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Text(ctx, 10, 20, d.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted text visible")
	}
	s, repo, _, _ = fixture(t)
	d, _ = s.Upload(ctx, 10, 20, "x.txt", strings.NewReader("body"))
	repo.completeErr = ErrStorage
	if _, e = s.Process(ctx, 10, 20, d.ID); e == nil || repo.doc.Status != Failed || repo.text.ContentText != "" {
		t.Fatal("failed text commit marked ready")
	}
	s, repo, _, _ = fixture(t)
	d, _ = s.Upload(ctx, 10, 20, "x.txt", strings.NewReader("body"))
	c, cancel := context.WithCancel(ctx)
	s.extractor = extractFunc(func(context.Context, io.Reader, string) (ExtractedText, error) {
		cancel()
		return ExtractedText{}, context.Canceled
	})
	if _, e = s.Process(c, 10, 20, d.ID); !errors.Is(e, context.Canceled) || !repo.failLive || repo.doc.Status != Failed {
		t.Fatal("canceled process did not independently finalize")
	}
}

func TestProcessPreservesDeadlineAcrossDatabasePhases(t *testing.T) {
	for _, phase := range []string{"claim", "extract", "complete"} {
		t.Run(phase, func(t *testing.T) {
			s, repo, _, _ := fixture(t)
			d, err := s.Upload(context.Background(), 10, 20, "deadline.md", strings.NewReader("hello"))
			if err != nil {
				t.Fatal(err)
			}
			s.limits.ProcessTimeout = 15 * time.Millisecond
			repo.claimWait = phase == "claim"
			repo.completeWait = phase == "complete"
			if phase == "extract" {
				realExtractor := s.extractor
				s.extractor = extractFunc(func(ctx context.Context, r io.Reader, media string) (ExtractedText, error) {
					x, err := realExtractor.Extract(ctx, r, media)
					<-ctx.Done()
					return x, err
				})
			}
			_, err = s.Process(context.Background(), 10, 20, d.ID)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost deadline: %v", err)
			}
			if phase == "claim" {
				if repo.doc.Status != Uploaded {
					t.Fatal("failed claim changed state")
				}
			} else if repo.doc.Status != Failed || repo.doc.FailureCode == nil || *repo.doc.FailureCode != "PROCESS_TIMEOUT" || !repo.failLive {
				t.Fatalf("incorrect independent failure state: %+v", repo.doc)
			}
		})
	}
}

func TestProcessSourceChangeIsPermanentAndCleanupRetainsGuard(t *testing.T) {
	s, repo, _, _ := fixture(t)
	d, e := s.Upload(context.Background(), 10, 20, "x.txt", strings.NewReader("original"))
	if e != nil {
		t.Fatal(e)
	}
	repo.doc.SHA256Hex = strings.Repeat("0", 64)
	g := taskguard.Guard{JobID: 1, OrgID: 20, DocID: d.ID, RunVersion: 1, ActorID: 10, Token: strings.Repeat("a", 32)}
	_, e = s.Process(taskguard.With(context.Background(), g), 10, 20, d.ID)
	if !errors.Is(e, ErrSourceChanged) || !errors.Is(e, ErrStorage) {
		t.Fatalf("wrong source sentinel: %v", e)
	}
	if repo.failGuard != g {
		t.Fatal("failure cleanup discarded task guard")
	}
}

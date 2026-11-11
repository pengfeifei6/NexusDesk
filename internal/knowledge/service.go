package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/cache"
	"github.com/pengfeifei6/NexusDesk/internal/platform/storage"
	"io"
	"log/slog"
	"math"
	"os"
	"time"
)

type MembershipFinder interface {
	FindActiveMembership(context.Context, int64, int64) (organization.Membership, error)
}
type Store interface {
	Create(context.Context, Document) (Document, error)
	Get(context.Context, int64, int64) (Document, error)
	List(context.Context, int64, Filter) ([]Document, error)
	ClaimProcess(context.Context, int64, int64) (Document, error)
	CompleteProcess(context.Context, Document, ExtractedText) (Document, error)
	FailProcess(context.Context, int64, int64, string) error
	Delete(context.Context, int64, int64) error
	GetText(context.Context, int64, int64) (DocumentText, error)
}
type Service struct {
	cache     *cache.Cache
	store     Store
	members   MembershipFinder
	objects   storage.ObjectStorage
	extractor TextExtractor
	limits    Limits
}

func NewService(repo Store, members MembershipFinder, objects storage.ObjectStorage, extractor TextExtractor, limits Limits) *Service {
	return &Service{store: repo, members: members, objects: objects, extractor: extractor, limits: limits}
}

// WithCache optionally enables authorized list caching during service setup.
func (s *Service) WithCache(c *cache.Cache) *Service { s.cache = c; return s }
func (s *Service) membership(ctx context.Context, actor, org int64) (organization.Membership, error) {
	if err := ctx.Err(); err != nil {
		return organization.Membership{}, err
	}
	if actor <= 0 {
		return organization.Membership{}, ErrUnauthenticated
	}
	if org <= 0 {
		return organization.Membership{}, ErrInvalidRequest
	}
	m, e := s.members.FindActiveMembership(ctx, org, actor)
	if errors.Is(e, organization.ErrNotFound) {
		return m, ErrNotFound
	}
	if e != nil {
		return m, ErrStorage
	}
	if m.OrgID != org || m.UserID != actor {
		return m, ErrStorage
	}
	if m.Status != organization.StatusActive {
		return m, ErrNotFound
	}
	if m.Role != organization.RoleAdmin && m.Role != organization.RoleMember {
		return m, ErrForbidden
	}
	return m, nil
}
func (s *Service) Authorize(ctx context.Context, actor, org int64) error {
	_, err := s.membership(ctx, actor, org)
	return err
}
func (s *Service) Upload(ctx context.Context, actor, org int64, name string, source io.Reader) (Document, error) {
	if err := s.Authorize(ctx, actor, org); err != nil {
		return Document{}, err
	}
	media, reader, err := uploadReader(name, source)
	if err != nil {
		return Document{}, err
	}
	key, err := storage.NewKey()
	if err != nil {
		return Document{}, ErrStorage
	}
	obj, err := s.objects.Put(ctx, key, reader, s.limits.MaxFileBytes)
	if errors.Is(err, storage.ErrTooLarge) {
		return Document{}, ErrTooLarge
	}
	if errors.Is(err, storage.ErrEmpty) {
		return Document{}, ErrInvalidRequest
	}
	if err != nil {
		return Document{}, err
	}
	d := Document{OrgID: org, UploadedBy: actor, OriginalFilename: name, StorageKey: key, MediaType: media, SizeBytes: obj.SizeBytes, SHA256Hex: obj.SHA256Hex, Status: Uploaded}
	result, err := s.store.Create(ctx, d)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		reason := ""
		if errors.Is(err, ErrUncertain) {
			reason = "metadata_outcome_unknown"
		} else if e := s.objects.Delete(cleanup, key); e != nil {
			reason = "delete_failed"
		}
		if reason != "" {
			journalCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if e := s.objects.RecordCleanup(journalCtx, key, reason); e != nil {
				slog.Error("pending object cleanup journal failed", "storage_key", key, "reason", reason)
			} else {
				slog.Warn("object pending reconciliation", "storage_key", key, "reason", reason)
			}
		}
		return Document{}, ErrStorage
	}
	return result, nil
}
func (s *Service) Get(ctx context.Context, actor, org, id int64) (Document, error) {
	if e := s.Authorize(ctx, actor, org); e != nil {
		return Document{}, e
	}
	if id <= 0 {
		return Document{}, ErrInvalidRequest
	}
	return s.store.Get(ctx, org, id)
}
func (s *Service) List(ctx context.Context, actor, org int64, f Filter) ([]Document, error) {
	if e := s.Authorize(ctx, actor, org); e != nil {
		return nil, e
	}
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 || (f.Page-1) > math.MaxInt64/f.PageSize {
		return nil, ErrInvalidRequest
	}
	if f.Status != "" && f.Status != Uploaded && f.Status != Processing && f.Status != Ready && f.Status != Failed {
		return nil, ErrInvalidRequest
	}
	result, err := cache.List(ctx, s.cache, org, "documents", f, func() ([]Document, error) {
		return s.store.List(ctx, org, f)
	}, func(item Document) bool { return item.OrgID == org && item.ID > 0 && item.DeletedAt == nil })
	if result == nil && err == nil {
		result = []Document{}
	}
	return result, err
}
func (s *Service) Delete(ctx context.Context, actor, org, id int64) error {
	m, e := s.membership(ctx, actor, org)
	if e != nil {
		return e
	}
	if id <= 0 {
		return ErrInvalidRequest
	}
	d, e := s.store.Get(ctx, org, id)
	if e != nil {
		return e
	}
	if d.UploadedBy != actor && m.Role != organization.RoleAdmin {
		return ErrForbidden
	}
	return s.store.Delete(ctx, org, id)
}
func (s *Service) Text(ctx context.Context, actor, org, id int64) (DocumentText, error) {
	d, e := s.Get(ctx, actor, org, id)
	if e != nil {
		return DocumentText{}, e
	}
	if d.Status != Ready {
		return DocumentText{}, ErrConflict
	}
	return s.store.GetText(ctx, org, id)
}
func (s *Service) Download(ctx context.Context, actor, org, id int64) (Document, io.ReadCloser, error) {
	d, e := s.Get(ctx, actor, org, id)
	if e != nil {
		return Document{}, nil, e
	}
	reader, e := s.objects.Open(ctx, d.StorageKey)
	if e != nil {
		return Document{}, nil, ErrStorage
	}
	if f, ok := reader.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := f.Stat()
		if err != nil || info.Size() != d.SizeBytes {
			reader.Close()
			return Document{}, nil, ErrStorage
		}
	}
	return d, reader, nil
}
func (s *Service) failed(origin context.Context, d Document, code string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(origin), 3*time.Second)
	defer cancel()
	if err := s.store.FailProcess(ctx, d.OrgID, d.ID, code); err != nil {
		slog.Error("document failure state not persisted", "org_id", d.OrgID, "document_id", d.ID, "failure_code", code)
	}
}
func processError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func processFailureCode(err error, fallback string) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "PROCESS_TIMEOUT"
	}
	if errors.Is(err, context.Canceled) {
		return "PROCESS_CANCELED"
	}
	var extraction *ExtractionError
	if errors.As(err, &extraction) {
		return extraction.Code
	}
	return fallback
}
func (s *Service) Process(ctx context.Context, actor, org, id int64) (Document, error) {
	if e := s.Authorize(ctx, actor, org); e != nil {
		return Document{}, e
	}
	if id <= 0 {
		return Document{}, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, s.limits.ProcessTimeout)
	defer cancel()
	d, err := s.store.ClaimProcess(ctx, org, id)
	if err != nil {
		return Document{}, processError(ctx, err)
	}
	reader, err := s.objects.Open(ctx, d.StorageKey)
	if err != nil {
		err = processError(ctx, ErrStorage)
		s.failed(ctx, d, processFailureCode(err, "SOURCE_UNAVAILABLE"))
		return Document{}, err
	}
	defer reader.Close()
	hash := sha256.New()
	extracted, err := s.extractor.Extract(ctx, io.TeeReader(reader, hash), d.MediaType)
	err = processError(ctx, err)
	if err == nil && hex.EncodeToString(hash.Sum(nil)) != d.SHA256Hex {
		s.failed(ctx, d, "SOURCE_CHANGED")
		return Document{}, ErrSourceChanged
	}
	if err != nil {
		s.failed(ctx, d, processFailureCode(err, "PROCESS_FAILED"))
		return Document{}, err
	}
	if err = ctx.Err(); err != nil {
		s.failed(ctx, d, processFailureCode(err, "PROCESS_FAILED"))
		return Document{}, err
	}
	result, err := s.store.CompleteProcess(ctx, d, extracted)
	if err != nil {
		err = processError(ctx, err)
		s.failed(ctx, d, processFailureCode(err, "PROCESS_FAILED"))
		return Document{}, err
	}
	return result, nil
}

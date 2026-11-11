package knowledge

import "time"

type Status string

const (
	Uploaded   Status = "UPLOADED"
	Processing Status = "PROCESSING"
	Ready      Status = "READY"
	Failed     Status = "FAILED"
)

type Document struct {
	ID               int64      `json:"id"`
	OrgID            int64      `json:"org_id"`
	UploadedBy       int64      `json:"uploaded_by"`
	OriginalFilename string     `json:"original_filename"`
	StorageKey       string     `json:"-"`
	MediaType        string     `json:"media_type"`
	SizeBytes        int64      `json:"size_bytes"`
	SHA256Hex        string     `json:"sha256_hex"`
	Status           Status     `json:"status"`
	FailureCode      *string    `json:"failure_code"`
	ExtractedChars   int64      `json:"extracted_chars"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"-"`
}
type DocumentText struct {
	DocumentID       int64     `json:"document_id"`
	ContentText      string    `json:"content_text"`
	ContentSHA256    string    `json:"content_sha256"`
	ExtractorVersion string    `json:"extractor_version"`
	ExtractedAt      time.Time `json:"extracted_at"`
}
type Filter struct {
	Page, PageSize int64
	Status         Status
}
type Limits struct {
	MaxFileBytes, MaxRequestBytes int64
	ProcessTimeout                time.Duration
}

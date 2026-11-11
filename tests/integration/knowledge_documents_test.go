package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/platform/storage"
)

// These tests require the isolated runner, never a developer's database or files.
func week05DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W05_KNOWLEDGE") != "1" {
		t.Skip("requires scripts/week05-smoke.py")
	}
	return ticketDB(t) // Retains the loopback and dedicated database-name guards.
}

type knowledgeFixture struct {
	pool                                             *pgxpool.Pool
	ctx                                              context.Context
	dir                                              string
	objects                                          *storage.Local
	repo                                             *knowledge.PostgresRepository
	members                                          *organization.PostgresRepository
	users                                            *auth.PostgresRepository
	tokens                                           *auth.TokenManager
	admin, uploader, peer, foreign                   int64
	adminToken, uploadToken, peerToken, foreignToken string
	org, otherOrg                                    int64
	limits                                           knowledge.Limits
	service                                          *knowledge.Service
	router                                           http.Handler
}

func newKnowledgeFixture(t *testing.T) *knowledgeFixture {
	t.Helper()
	f := &knowledgeFixture{pool: week05DB(t)}
	var cancel context.CancelFunc
	f.ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	f.dir = t.TempDir()
	if os.Chmod(f.dir, 0700) != nil {
		t.Fatal("cannot secure private fixture directory")
	}
	var err error
	f.objects, err = storage.NewLocal(f.dir)
	if err != nil {
		t.Fatal("cannot create private fixture storage")
	}
	t.Cleanup(func() { _ = f.objects.Close() })
	f.repo = knowledge.NewPostgresRepository(f.pool)
	f.members = organization.NewPostgresRepository(f.pool)
	f.users = auth.NewPostgresRepository(f.pool)
	// Public synthetic test data only; no real password hashing or credentials.
	f.tokens, err = auth.NewTokenManager(auth.TokenConfig{Issuer: "week05-fixture", Audience: "week05-fixture", TTL: 15 * time.Minute, Secret: bytes.Repeat([]byte{0x57}, 32)})
	if err != nil {
		t.Fatal("cannot create synthetic token manager")
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 10)
	makeUser := func(label string) (int64, string) {
		var id int64
		if f.pool.QueryRow(f.ctx, "INSERT INTO users(email,display_name,password_hash) VALUES($1,$2,'not-a-credential') RETURNING id", "w05-"+label+tag+"@example.test", label).Scan(&id) != nil {
			t.Fatal("cannot create synthetic user")
		}
		token, err := f.tokens.Issue(id)
		if err != nil {
			t.Fatal("cannot issue synthetic identity")
		}
		return id, token.AccessToken
	}
	f.admin, f.adminToken = makeUser("admin")
	f.uploader, f.uploadToken = makeUser("uploader")
	f.peer, f.peerToken = makeUser("peer")
	f.foreign, f.foreignToken = makeUser("foreign")
	for _, out := range []*int64{&f.org, &f.otherOrg} {
		if f.pool.QueryRow(f.ctx, "INSERT INTO organizations(name) VALUES('W05 synthetic organization') RETURNING id").Scan(out) != nil {
			t.Fatal("cannot create fixture organization")
		}
	}
	f.exec(t, "INSERT INTO organization_members(org_id,user_id,role) VALUES($1,$2,'admin'),($1,$3,'member'),($1,$4,'member'),($5,$6,'admin')", f.org, f.admin, f.uploader, f.peer, f.otherOrg, f.foreign)
	f.limits = knowledge.Limits{MaxFileBytes: 5 << 20, MaxRequestBytes: 8 << 20, ProcessTimeout: 10 * time.Second}
	f.service = f.newService(knowledge.NewTextExtractor(5<<20, 1<<20), f.limits)
	f.router = f.routerFor(f.service)
	return f
}

func (f *knowledgeFixture) newService(x knowledge.TextExtractor, limits knowledge.Limits) *knowledge.Service {
	return knowledge.NewService(f.repo, f.members, f.objects, x, limits)
}

func (f *knowledgeFixture) routerFor(s *knowledge.Service) http.Handler {
	return httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(f.tokens, f.users))
		knowledge.NewHandler(s).RegisterRoutes(g)
	})
}

func (f *knowledgeFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal("fixture SQL operation failed")
	}
}

func (f *knowledgeFixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if f.pool.QueryRow(f.ctx, sql, args...).Scan(&n) != nil {
		t.Fatal("fixture count failed")
	}
	return n
}

func (f *knowledgeFixture) base(org int64) string {
	return fmt.Sprintf("/api/v1/organizations/%d/knowledge/documents", org)
}
func (f *knowledgeFixture) path(org, id int64) string {
	return f.base(org) + "/" + strconv.FormatInt(id, 10)
}

func knowledgeCall(router http.Handler, token, method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func (f *knowledgeFixture) call(token, method, path string) *httptest.ResponseRecorder {
	return knowledgeCall(f.router, token, method, path, nil, "")
}

func knowledgeExpect(t *testing.T, rec *httptest.ResponseRecorder, want int, code string) {
	t.Helper()
	if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("HTTP status/cache policy mismatch: got %d, want %d", rec.Code, want)
	}
	if want >= 400 {
		var e struct {
			Error struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &e) != nil || e.Error.Code != code || e.Error.Message == "" {
			t.Fatal("unexpected error envelope/code")
		}
		var fields struct {
			Error map[string]json.RawMessage `json:"error"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &fields) != nil || len(fields.Error) != 3 || fields.Error["request_id"] == nil {
			t.Fatal("error envelope fields mismatch")
		}
		for _, forbidden := range []string{"private SQL fixture", "storage_key", "password_hash", "not-a-credential", "SELECT ", "INSERT ", "SQLSTATE"} {
			if strings.Contains(rec.Body.String(), forbidden) {
				t.Fatal("internal error details exposed")
			}
		}
	}
}

func knowledgeData(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &envelope) != nil || len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, out) != nil {
		t.Fatal("invalid success envelope")
	}
}

func knowledgeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func knowledgeMultipart(t *testing.T, name string, content []byte, partType string, extra bool) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
	h.Set("Content-Type", partType)
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatal("cannot create multipart fixture")
	}
	if _, err = part.Write(content); err != nil {
		t.Fatal("cannot write multipart fixture")
	}
	if extra {
		if w.WriteField("unexpected", "extra") != nil {
			t.Fatal("cannot add multipart fixture part")
		}
	}
	if w.Close() != nil {
		t.Fatal("cannot close multipart fixture")
	}
	return buf, w.FormDataContentType()
}

func (f *knowledgeFixture) upload(t *testing.T, token string, org int64, name string, content []byte) knowledge.Document {
	t.Helper()
	body, ct := knowledgeMultipart(t, name, content, "application/octet-stream", false)
	rec := knowledgeCall(f.router, token, "POST", f.base(org), body, ct)
	knowledgeExpect(t, rec, 201, "")
	var d knowledge.Document
	knowledgeData(t, rec, &d)
	if d.ID <= 0 || d.OrgID != org || d.Status != knowledge.Uploaded || d.SHA256Hex != knowledgeHash(content) || d.SizeBytes != int64(len(content)) || d.FailureCode != nil {
		t.Fatal("upload metadata mismatch")
	}
	return d
}

func (f *knowledgeFixture) key(t *testing.T, d knowledge.Document) string {
	t.Helper()
	var key string
	if f.pool.QueryRow(f.ctx, "SELECT storage_key FROM knowledge_documents WHERE org_id=$1 AND id=$2", d.OrgID, d.ID).Scan(&key) != nil {
		t.Fatal("cannot locate fixture object")
	}
	return key
}

func (f *knowledgeFixture) assertObject(t *testing.T, d knowledge.Document, content []byte) {
	t.Helper()
	path := filepath.Join(f.dir, f.key(t, d))
	got, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || !bytes.Equal(got, content) || info.Mode().Perm() != 0600 || knowledgeHash(got) != d.SHA256Hex {
		t.Fatal("private object bytes/hash/permissions mismatch")
	}
}

func (f *knowledgeFixture) assertNoText(t *testing.T, d knowledge.Document, failure string) {
	t.Helper()
	var status string
	var code *string
	if f.pool.QueryRow(f.ctx, "SELECT status,failure_code FROM knowledge_documents WHERE org_id=$1 AND id=$2", d.OrgID, d.ID).Scan(&status, &code) != nil {
		t.Fatal("cannot inspect processing result")
	}
	if status == "READY" || f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE org_id=$1 AND document_id=$2", d.OrgID, d.ID) != 0 {
		t.Fatal("failed processing published READY or text")
	}
	if failure != "" && (status != "FAILED" || code == nil || *code != failure) {
		t.Fatal("independent failure state/code was not persisted")
	}
}

// Failure triggers exercise PostgreSQL INSERT and deferred COMMIT failures.
// Tests are deliberately sequential because a trigger affects the whole table.
func (f *knowledgeFixture) failureTrigger(t *testing.T, table string, deferred bool) func() {
	t.Helper()
	f.exec(t, `CREATE FUNCTION nexusdesk_w05_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private SQL fixture'; END $$`)
	ddl := "CREATE TRIGGER nexusdesk_w05_failure BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION nexusdesk_w05_fail()"
	if deferred {
		ddl = "CREATE CONSTRAINT TRIGGER nexusdesk_w05_failure AFTER INSERT ON " + table + " DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nexusdesk_w05_fail()"
	}
	f.exec(t, ddl)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := f.pool.Exec(ctx, "DROP TRIGGER nexusdesk_w05_failure ON "+table); err != nil {
				t.Error("cannot remove fault trigger")
			}
			if _, err := f.pool.Exec(ctx, "DROP FUNCTION nexusdesk_w05_fail()"); err != nil {
				t.Error("cannot remove fault function")
			}
		})
	}
	t.Cleanup(cleanup)
	return cleanup
}

func TestWeek05Schema(t *testing.T) {
	f := newKnowledgeFixture(t)
	if f.count(t, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('knowledge_documents','knowledge_document_texts')") != 2 {
		t.Fatal("Week 05 tables missing")
	}
	d := f.upload(t, f.uploadToken, f.org, "schema.md", []byte("# synthetic schema fixture\n"))
	t.Run("defaults_and_scoped_parent", func(t *testing.T) {
		if d.UploadedBy != f.uploader || d.ExtractedChars != 0 || d.CreatedAt.IsZero() || d.UpdatedAt.IsZero() || d.MediaType != "text/markdown" {
			t.Fatal("schema defaults not returned")
		}
	})
	bad := func(name, code, sql string, args ...any) {
		t.Run(name, func(t *testing.T) {
			_, err := f.pool.Exec(f.ctx, sql, args...)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != code {
				t.Fatalf("expected SQLSTATE %s", code)
			}
		})
	}
	for _, tc := range []struct{ name, expr string }{
		{"filename_empty", "original_filename=''"}, {"filename_byte_limit", "original_filename=repeat('中',67)"},
		{"storage_key_shape", "storage_key='../unsafe'"}, {"media_type", "media_type='text/html'"},
		{"empty_size", "size_bytes=0"}, {"negative_size", "size_bytes=-1"}, {"size_ceiling", "size_bytes=67108865"},
		{"sha_shape", "sha256_hex='invalid'"}, {"status_enum", "status='OTHER'"},
		{"failed_requires_code", "status='FAILED'"}, {"failure_code_enum", "status='FAILED',failure_code='arbitrary private error'"},
		{"code_requires_failed", "failure_code='PROCESS_FAILED'"}, {"ready_requires_chars", "status='READY'"},
		{"nonready_zero_chars", "extracted_chars=1"}, {"negative_chars", "extracted_chars=-1"},
		{"chars_ceiling", "status='READY',extracted_chars=1048577"},
	} {
		bad("CHECK_"+tc.name, "23514", "UPDATE knowledge_documents SET "+tc.expr+" WHERE org_id=$1 AND id=$2", f.org, d.ID)
	}
	bad("FK_uploader_org", "23503", "UPDATE knowledge_documents SET uploaded_by=$1 WHERE org_id=$2 AND id=$3", f.foreign, f.org, d.ID)
	bad("FK_missing_org", "23503", "UPDATE knowledge_documents SET org_id=9223372036854775807 WHERE org_id=$1 AND id=$2", f.org, d.ID)
	other := f.upload(t, f.uploadToken, f.org, "other.txt", []byte("other synthetic schema fixture"))
	bad("UNIQUE_storage_key", "23505", "UPDATE knowledge_documents SET storage_key=$1 WHERE org_id=$2 AND id=$3", f.key(t, d), f.org, other.ID)
	textSQL := "INSERT INTO knowledge_document_texts(org_id,document_id,content_text,content_sha256,extractor_version) VALUES($1,$2,$3,$4,$5)"
	bad("FK_text_parent_org", "23503", textSQL, f.otherOrg, d.ID, "fixture", knowledgeHash([]byte("fixture")), "utf8-v1")
	bad("FK_text_missing_parent", "23503", textSQL, f.org, int64(9223372036854775807), "fixture", knowledgeHash([]byte("fixture")), "utf8-v1")
	for _, tc := range []struct{ name, content, hash, version string }{
		{"empty", "", strings.Repeat("a", 64), "utf8-v1"}, {"blank", "   ", strings.Repeat("a", 64), "utf8-v1"},
		{"too_large", strings.Repeat("x", (1<<20)+1), strings.Repeat("a", 64), "utf8-v1"},
		{"hash", "fixture", "invalid", "utf8-v1"}, {"version", "fixture", strings.Repeat("a", 64), "other"},
	} {
		bad("CHECK_text_"+tc.name, "23514", textSQL, f.org, d.ID, tc.content, tc.hash, tc.version)
	}
	f.exec(t, textSQL, f.org, d.ID, "fixture", knowledgeHash([]byte("fixture")), "utf8-v1")
	bad("UNIQUE_text_document", "23505", textSQL, f.org, d.ID, "fixture", knowledgeHash([]byte("fixture")), "utf8-v1")
	t.Run("partial_ordering_indexes", func(t *testing.T) {
		for _, name := range []string{"knowledge_documents_org_created_idx", "knowledge_documents_org_status_idx"} {
			var definition string
			if f.pool.QueryRow(f.ctx, "SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND indexname=$1", name).Scan(&definition) != nil || !strings.Contains(definition, "org_id") || !strings.Contains(definition, "created_at DESC, id DESC") || !strings.Contains(definition, "WHERE (deleted_at IS NULL)") {
				t.Fatal("required partial ordering index missing")
			}
			if strings.Contains(name, "status") && !strings.Contains(definition, "org_id, status,") {
				t.Fatal("status filter index missing")
			}
		}
	})
}

func TestWeek05Knowledge(t *testing.T) {
	f := newKnowledgeFixture(t)
	t.Run("B01_B02_upload_TXT_MD_real_private_bytes_hash", func(t *testing.T) {
		for _, tc := range []struct{ name, media string }{{"fixture.TXT", "text/plain"}, {"fixture.MD", "text/markdown"}} {
			content := []byte("# Synthetic Week 05 fixture\n组织内知识正文\n")
			d := f.upload(t, f.uploadToken, f.org, tc.name, content)
			if d.UploadedBy != f.uploader || d.MediaType != tc.media {
				t.Fatal("upload identity/media mismatch")
			}
			f.assertObject(t, d, content)
		}
	})
	t.Run("B03_list_paging_status_and_stable_id_tiebreak", func(t *testing.T) {
		first := f.upload(t, f.uploadToken, f.org, "page-first.md", []byte("first"))
		second := f.upload(t, f.uploadToken, f.org, "page-second.md", []byte("second"))
		foreign := f.upload(t, f.foreignToken, f.otherOrg, "foreign.md", []byte("foreign"))
		var defaults struct {
			Items    []knowledge.Document `json:"items"`
			Page     int                  `json:"page"`
			PageSize int                  `json:"page_size"`
		}
		defaultResponse := f.call(f.foreignToken, "GET", f.base(f.otherOrg))
		knowledgeExpect(t, defaultResponse, 200, "")
		knowledgeData(t, defaultResponse, &defaults)
		if defaults.Page != 1 || defaults.PageSize != 20 || len(defaults.Items) != 1 || defaults.Items[0].ID != foreign.ID {
			t.Fatal("default paging or organization scope mismatch")
		}
		f.exec(t, "UPDATE knowledge_documents SET created_at='2099-01-01T00:00:00Z' WHERE org_id=$1 AND id IN($2,$3)", f.org, first.ID, second.ID)
		for _, tc := range []struct {
			page int
			id   int64
		}{{1, second.ID}, {2, first.ID}} {
			rec := f.call(f.peerToken, "GET", f.base(f.org)+fmt.Sprintf("?page=%d&page_size=1&status=UPLOADED", tc.page))
			knowledgeExpect(t, rec, 200, "")
			var envelope struct {
				Items    []knowledge.Document `json:"items"`
				Page     int                  `json:"page"`
				PageSize int                  `json:"page_size"`
			}
			knowledgeData(t, rec, &envelope)
			if len(envelope.Items) != 1 || envelope.Items[0].ID != tc.id || envelope.Items[0].OrgID != f.org || envelope.Page != tc.page || envelope.PageSize != 1 {
				t.Fatal("scoped stable pagination mismatch")
			}
		}
		knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, second.ID)+"/process"), 200, "")
		rec := f.call(f.peerToken, "GET", f.base(f.org)+"?status=READY")
		knowledgeExpect(t, rec, 200, "")
		var result struct {
			Items []knowledge.Document `json:"items"`
		}
		knowledgeData(t, rec, &result)
		if len(result.Items) != 1 || result.Items[0].ID != second.ID {
			t.Fatal("status filter leaked rows")
		}
		for _, q := range []string{"?page=0", "?page_size=101", "?page=1&page=2", "?status=OTHER", "?unknown=1", "?page=9223372036854775807&page_size=100"} {
			knowledgeExpect(t, f.call(f.uploadToken, "GET", f.base(f.org)+q), 400, "INVALID_REQUEST")
		}
		rec = f.call(f.peerToken, "GET", f.base(f.org)+"?page=100&page_size=100")
		knowledgeExpect(t, rec, 200, "")
		knowledgeData(t, rec, &defaults)
		if defaults.Items == nil || len(defaults.Items) != 0 {
			t.Fatal("empty page must be a JSON array")
		}
	})
	t.Run("B04_detail_and_list_DTO_never_expose_storage_or_text", func(t *testing.T) {
		d := f.upload(t, f.uploadToken, f.org, "dto.md", []byte("private synthetic document body"))
		key := f.key(t, d)
		for _, target := range []string{f.path(f.org, d.ID), f.base(f.org)} {
			rec := f.call(f.peerToken, "GET", target)
			knowledgeExpect(t, rec, 200, "")
			for _, secret := range []string{key, f.dir, "storage_key", "deleted_at", "content_text", "password_hash", "private synthetic document body"} {
				if strings.Contains(rec.Body.String(), secret) {
					t.Fatal("DTO contains private storage or document body")
				}
			}
		}
		var fields map[string]json.RawMessage
		knowledgeData(t, f.call(f.peerToken, "GET", f.path(f.org, d.ID)), &fields)
		want := []string{"id", "org_id", "uploaded_by", "original_filename", "media_type", "size_bytes", "sha256_hex", "status", "failure_code", "extracted_chars", "created_at", "updated_at"}
		if len(fields) != len(want) {
			t.Fatal("document DTO field count mismatch")
		}
		for _, key := range want {
			if _, ok := fields[key]; !ok {
				t.Fatal("document DTO required field missing")
			}
		}
	})
	t.Run("B05_all_cross_org_document_operations_404", func(t *testing.T) {
		d := f.upload(t, f.uploadToken, f.org, "scope.md", []byte("scope fixture"))
		// Source access would fail, but invisible resources must still return 404.
		object := filepath.Join(f.dir, f.key(t, d))
		if os.Rename(object, object+".held") != nil {
			t.Fatal("cannot hold synthetic scope object")
		}
		defer func() {
			if os.Rename(object+".held", object) != nil {
				t.Error("cannot restore synthetic scope object")
			}
		}()
		for _, op := range knowledgeDocumentOperations() {
			for _, org := range []int64{f.org, f.otherOrg} {
				knowledgeExpect(t, f.call(f.foreignToken, op.method, f.path(org, d.ID)+op.suffix), 404, "NOT_FOUND")
			}
		}
	})
	t.Run("B06_disabled_membership_rechecks_preexisting_token_all_operations", func(t *testing.T) {
		d := f.upload(t, f.peerToken, f.org, "revoked.md", []byte("revoked fixture"))
		f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.peer)
		defer f.exec(t, "UPDATE organization_members SET status='active' WHERE org_id=$1 AND user_id=$2", f.org, f.peer)
		for _, op := range knowledgeDocumentOperations() {
			knowledgeExpect(t, f.call(f.peerToken, op.method, f.path(f.org, d.ID)+op.suffix), 404, "NOT_FOUND")
		}
		knowledgeExpect(t, f.call(f.peerToken, "GET", f.base(f.org)), 404, "NOT_FOUND")
		body, ct := knowledgeMultipart(t, "revoked.txt", []byte("fixture"), "text/plain", false)
		knowledgeExpect(t, knowledgeCall(f.router, f.peerToken, "POST", f.base(f.org), body, ct), 404, "NOT_FOUND")
	})
	t.Run("B07_B08_delete_policy_soft_delete_hides_all_operations_retains_objects_text", func(t *testing.T) {
		for _, token := range []string{f.uploadToken, f.adminToken} {
			d := f.upload(t, f.uploadToken, f.org, "delete.md", []byte("delete retained fixture"))
			knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, d.ID)+"/process"), 200, "")
			knowledgeExpect(t, f.call(f.peerToken, "DELETE", f.path(f.org, d.ID)), 403, "FORBIDDEN")
			rec := f.call(token, "DELETE", f.path(f.org, d.ID))
			knowledgeExpect(t, rec, 204, "")
			if rec.Body.Len() != 0 {
				t.Fatal("204 has response body")
			}
			for _, op := range knowledgeDocumentOperations() {
				knowledgeExpect(t, f.call(f.adminToken, op.method, f.path(f.org, d.ID)+op.suffix), 404, "NOT_FOUND")
			}
			if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND deleted_at IS NOT NULL", f.org, d.ID) != 1 || f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 1 {
				t.Fatal("soft delete removed retained metadata/text")
			}
			var list struct {
				Items []knowledge.Document `json:"items"`
			}
			knowledgeData(t, f.call(f.adminToken, "GET", f.base(f.org)), &list)
			for _, item := range list.Items {
				if item.ID == d.ID {
					t.Fatal("deleted document appears in list")
				}
			}
			f.assertObject(t, d, []byte("delete retained fixture"))
		}
	})
	t.Run("S01_S02_exact_default_5MiB_and_plus_one", func(t *testing.T) {
		content := bytes.Repeat([]byte("a"), 5<<20)
		d := f.upload(t, f.uploadToken, f.org, "exact.txt", content)
		f.assertObject(t, d, content)
		beforeRows := f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org)
		beforeFiles := knowledgeFileCount(t, f.dir)
		body, ct := knowledgeMultipart(t, "too-large.txt", append(content, 'b'), "text/plain", false)
		knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, "POST", f.base(f.org), body, ct), 413, "DOCUMENT_TOO_LARGE")
		if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org) != beforeRows || knowledgeFileCount(t, f.dir) != beforeFiles {
			t.Fatal("oversize upload left metadata/object")
		}
	})
	t.Run("S03_S04_S05_S06_S07_P02_invalid_uploads_leave_no_rows_or_files", func(t *testing.T) {
		for _, tc := range []struct {
			name, filename, media string
			body                  []byte
			want                  int
			code                  string
			extra                 bool
		}{
			{"exe", "fixture.exe", "text/plain", []byte("fixture"), 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"html", "fixture.html", "text/plain", []byte("fixture"), 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"binary_md", "fixture.md", "text/markdown", []byte{'P', 'K', 3, 4, 0, 1, 2, 3}, 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"nul_md", "fixture.md", "text/markdown", []byte("valid prefix\x00binary"), 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"MIME_spoof_pdf", "fixture.md", "text/plain", []byte("%PDF-1.7\nfixture"), 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"MIME_spoof_binary", "fixture.txt", "text/plain", []byte{0x89, 'P', 'N', 'G', 1, 2, 3, 4}, 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"invalid_UTF8", "fixture.md", "text/markdown", []byte{'a', 0xff, 'b'}, 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"late_invalid_UTF8", "fixture.md", "text/plain", append(bytes.Repeat([]byte("a"), 32768), 0xff), 415, "UNSUPPORTED_DOCUMENT_TYPE", false},
			{"traversal", "../../x.md", "text/markdown", []byte("fixture"), 400, "INVALID_REQUEST", false},
			{"backslash", `..\x.md`, "text/markdown", []byte("fixture"), 400, "INVALID_REQUEST", false},
			{"empty", "empty.md", "text/markdown", nil, 400, "INVALID_REQUEST", false},
			{"extra_part", "fixture.md", "text/markdown", []byte("fixture"), 400, "INVALID_REQUEST", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				beforeRows := f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org)
				beforeFiles := knowledgeFileCount(t, f.dir)
				body, ct := knowledgeMultipart(t, tc.filename, tc.body, tc.media, tc.extra)
				knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, "POST", f.base(f.org), body, ct), tc.want, tc.code)
				if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org) != beforeRows || knowledgeFileCount(t, f.dir) != beforeFiles {
					t.Fatal("rejected upload left metadata/object")
				}
			})
		}
		buf := new(bytes.Buffer)
		w := multipart.NewWriter(buf)
		if w.WriteField("description", "missing file") != nil || w.Close() != nil {
			t.Fatal("cannot build missing-file fixture")
		}
		knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, "POST", f.base(f.org), buf, w.FormDataContentType()), 400, "INVALID_REQUEST")
		knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, "POST", f.base(f.org), strings.NewReader("fixture"), "text/plain"), 415, "UNSUPPORTED_DOCUMENT_TYPE")
	})
	t.Run("S05_Content_Transfer_Encoding_rejected_before_metadata_or_object", func(t *testing.T) {
		for _, encoding := range []string{"quoted-printable", "base64", "binary", ""} {
			t.Run("encoding_"+encoding, func(t *testing.T) {
				beforeRows := f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org)
				beforeFiles := knowledgeFileCount(t, f.dir)
				body := new(bytes.Buffer)
				w := multipart.NewWriter(body)
				header := make(textproto.MIMEHeader)
				header.Set("Content-Disposition", `form-data; name="file"; filename="encoded.txt"`)
				header.Set("Content-Type", "text/plain")
				header.Set("Content-Transfer-Encoding", encoding)
				part, err := w.CreatePart(header)
				if err != nil {
					t.Fatal("cannot create transfer-encoding fixture")
				}
				if _, err = io.WriteString(part, "synthetic=20transfer=0Afixture"); err != nil {
					t.Fatal("cannot write transfer-encoding fixture")
				}
				if w.Close() != nil {
					t.Fatal("cannot close transfer-encoding fixture")
				}
				knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, "POST", f.base(f.org), body, w.FormDataContentType()), 400, "INVALID_REQUEST")
				if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org) != beforeRows || knowledgeFileCount(t, f.dir) != beforeFiles {
					t.Fatal("transfer-encoded upload left metadata/object")
				}
			})
		}
	})
	t.Run("S08_metadata_INSERT_and_deferred_COMMIT_failures_compensate_new_file", func(t *testing.T) {
		for _, deferred := range []bool{false, true} {
			t.Run(fmt.Sprintf("deferred_%v", deferred), func(t *testing.T) {
				cleanup := f.failureTrigger(t, "knowledge_documents", deferred)
				defer cleanup()
				beforeRows := f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org)
				beforeFiles := knowledgeFileCount(t, f.dir)
				body, ct := knowledgeMultipart(t, "insert-failure.md", []byte("safe SQL fault fixture"), "text/markdown", false)
				knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, "POST", f.base(f.org), body, ct), 500, "INTERNAL_ERROR")
				if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1", f.org) != beforeRows || knowledgeFileCount(t, f.dir) != beforeFiles {
					t.Fatal("failed metadata write left metadata or file")
				}
			})
		}
	})
	t.Run("authorized_download_original_bytes_attachment_nosniff_no_store", func(t *testing.T) {
		content := []byte("\xef\xbb\xbf# 原创 Markdown\r\n<script>fixture</script>\r\n")
		d := f.upload(t, f.uploadToken, f.org, "原创 fixture.md", content)
		knowledgeExpect(t, f.call("", "GET", f.path(f.org, d.ID)+"/download"), 401, "UNAUTHENTICATED")
		rec := f.call(f.peerToken, "GET", f.path(f.org, d.ID)+"/download")
		knowledgeExpect(t, rec, 200, "")
		disposition, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
		if err != nil || disposition != "attachment" || params["filename"] != d.OriginalFilename || rec.Header().Get("Content-Type") != "application/octet-stream" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Length") != strconv.Itoa(len(content)) || !bytes.Equal(rec.Body.Bytes(), content) || knowledgeHash(rec.Body.Bytes()) != d.SHA256Hex {
			t.Fatal("authorized download bytes or headers mismatch")
		}
	})
	t.Run("strict_authenticated_input_and_missing_ids", func(t *testing.T) {
		d := f.upload(t, f.uploadToken, f.org, "strict-input.md", []byte("strict input fixture"))
		for _, op := range knowledgeDocumentOperations() {
			path := f.path(f.org, d.ID) + op.suffix
			knowledgeExpect(t, f.call("", op.method, path), 401, "UNAUTHENTICATED")
			knowledgeExpect(t, f.call(f.uploadToken, op.method, path+"?extra=1"), 400, "INVALID_REQUEST")
			knowledgeExpect(t, knowledgeCall(f.router, f.uploadToken, op.method, path, strings.NewReader("{}"), "application/json"), 400, "INVALID_REQUEST")
			knowledgeExpect(t, f.call(f.uploadToken, op.method, f.path(f.org, 9223372036854775807)+op.suffix), 404, "NOT_FOUND")
			knowledgeExpect(t, f.call(f.uploadToken, op.method, f.base(f.org)+"/0"+op.suffix), 400, "INVALID_REQUEST")
		}
	})
	t.Run("P01_P06_READY_normalization_hash_chars_and_duplicate_conflict", func(t *testing.T) {
		content := []byte("\xef\xbb\xbf# 标题\r\n<script>synthetic</script>\r尾行\n")
		want := "# 标题\n<script>synthetic</script>\n尾行\n"
		d := f.upload(t, f.uploadToken, f.org, "normalize.md", content)
		knowledgeExpect(t, f.call(f.peerToken, "GET", f.path(f.org, d.ID)+"/text"), 409, "DOCUMENT_STATE_CONFLICT")
		rec := f.call(f.peerToken, "POST", f.path(f.org, d.ID)+"/process")
		knowledgeExpect(t, rec, 200, "")
		var ready knowledge.Document
		knowledgeData(t, rec, &ready)
		if ready.Status != knowledge.Ready || ready.ExtractedChars != int64(utf8.RuneCountInString(want)) || ready.FailureCode != nil {
			t.Fatal("READY metadata invalid")
		}
		rec = f.call(f.peerToken, "GET", f.path(f.org, d.ID)+"/text")
		knowledgeExpect(t, rec, 200, "")
		var text knowledge.DocumentText
		knowledgeData(t, rec, &text)
		if text.DocumentID != d.ID || text.ContentText != want || text.ContentSHA256 != knowledgeHash([]byte(want)) || text.ExtractorVersion != "utf8-v1" || text.ExtractedAt.IsZero() {
			t.Fatal("normalized text publication mismatch")
		}
		knowledgeExpect(t, f.call(f.peerToken, "POST", f.path(f.org, d.ID)+"/process"), 409, "DOCUMENT_STATE_CONFLICT")
		var after knowledge.DocumentText
		knowledgeData(t, f.call(f.peerToken, "GET", f.path(f.org, d.ID)+"/text"), &after)
		if !after.ExtractedAt.Equal(text.ExtractedAt) || after.ContentSHA256 != text.ContentSHA256 || f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 1 {
			t.Fatal("duplicate processing changed text")
		}
		f.assertObject(t, d, content)
	})
	t.Run("P03_P04_blank_and_over_limit_extraction_are_422_without_publication", func(t *testing.T) {
		for _, tc := range []struct {
			name, code string
			content    []byte
		}{
			{"blank.md", "EMPTY_TEXT", []byte(" \t\r\n ")}, {"large.md", "TEXT_TOO_LARGE", bytes.Repeat([]byte("x"), (1<<20)+1)},
			{"fixture.pdf", "PDF_UNSUPPORTED", []byte("%PDF-1.7\nsynthetic unsupported fixture\n")},
		} {
			d := f.upload(t, f.uploadToken, f.org, tc.name, tc.content)
			knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, d.ID)+"/process"), 422, tc.code)
			f.assertNoText(t, d, tc.code)
			f.assertObject(t, d, tc.content)
			knowledgeExpect(t, f.call(f.uploadToken, "GET", f.path(f.org, d.ID)+"/text"), 409, "DOCUMENT_STATE_CONFLICT")
		}
	})
	t.Run("P05_true_concurrent_claim_second_409_first_200_single_text", func(t *testing.T) {
		d := f.upload(t, f.uploadToken, f.org, "concurrent.md", []byte("concurrent processing fixture"))
		x := newKnowledgeBlockingExtractor()
		defer x.unblock()
		router := f.routerFor(f.newService(x, f.limits))
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- knowledgeCall(router, f.uploadToken, "POST", f.path(f.org, d.ID)+"/process", nil, "")
		}()
		knowledgeWaitEntered(t, x)
		if f.count(t, "SELECT count(*) FROM knowledge_documents WHERE org_id=$1 AND id=$2 AND status='PROCESSING'", f.org, d.ID) != 1 {
			t.Fatal("extractor ran before real ClaimProcess")
		}
		knowledgeExpect(t, knowledgeCall(router, f.peerToken, "POST", f.path(f.org, d.ID)+"/process", nil, ""), 409, "DOCUMENT_STATE_CONFLICT")
		x.unblock()
		knowledgeExpect(t, knowledgeWaitResponse(t, result), 200, "")
		if f.count(t, "SELECT count(*) FROM knowledge_document_texts WHERE org_id=$1 AND document_id=$2", f.org, d.ID) != 1 {
			t.Fatal("concurrent processing published multiple texts")
		}
	})
	t.Run("process_delete_race_never_publishes_READY_or_text_after_delete", func(t *testing.T) {
		content := []byte("process delete fixture")
		d := f.upload(t, f.uploadToken, f.org, "process-delete.md", content)
		x := newKnowledgeBlockingExtractor()
		defer x.unblock()
		router := f.routerFor(f.newService(x, f.limits))
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- knowledgeCall(router, f.uploadToken, "POST", f.path(f.org, d.ID)+"/process", nil, "")
		}()
		knowledgeWaitEntered(t, x)
		knowledgeExpect(t, f.call(f.uploadToken, "DELETE", f.path(f.org, d.ID)), 204, "")
		x.unblock()
		knowledgeExpect(t, knowledgeWaitResponse(t, result), 404, "NOT_FOUND")
		f.assertNoText(t, d, "")
		knowledgeExpect(t, f.call(f.uploadToken, "GET", f.path(f.org, d.ID)+"/text"), 404, "NOT_FOUND")
		f.assertObject(t, d, content)
	})
	t.Run("P07_text_INSERT_and_deferred_COMMIT_failure_roll_back_READY_and_text", func(t *testing.T) {
		for _, deferred := range []bool{false, true} {
			t.Run(fmt.Sprintf("deferred_%v", deferred), func(t *testing.T) {
				content := []byte("transaction rollback fixture")
				d := f.upload(t, f.uploadToken, f.org, "text-failure.md", content)
				cleanup := f.failureTrigger(t, "knowledge_document_texts", deferred)
				knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, d.ID)+"/process"), 500, "INTERNAL_ERROR")
				f.assertNoText(t, d, "PROCESS_FAILED")
				f.assertObject(t, d, content)
				cleanup()
				knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, d.ID)+"/process"), 200, "")
			})
		}
	})
	t.Run("P08_timeout_and_cancellation_persist_FAILED_independently", func(t *testing.T) {
		t.Run("timeout_HTTP_504", func(t *testing.T) {
			content := []byte("timeout fixture")
			d := f.upload(t, f.uploadToken, f.org, "timeout.txt", content)
			x := newKnowledgeBlockingExtractor()
			defer x.unblock()
			limits := f.limits
			limits.ProcessTimeout = 300 * time.Millisecond
			router := f.routerFor(f.newService(x, limits))
			knowledgeExpect(t, knowledgeCall(router, f.uploadToken, "POST", f.path(f.org, d.ID)+"/process", nil, ""), 504, "PROCESS_TIMEOUT")
			f.assertNoText(t, d, "PROCESS_TIMEOUT")
			f.assertObject(t, d, content)
		})
		t.Run("cancellation_after_claim", func(t *testing.T) {
			content := []byte("cancellation fixture")
			d := f.upload(t, f.uploadToken, f.org, "canceled.txt", content)
			x := newKnowledgeBlockingExtractor()
			defer x.unblock()
			service := f.newService(x, f.limits)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := service.Process(ctx, f.uploader, f.org, d.ID); result <- err }()
			knowledgeWaitEntered(t, x)
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("processing did not honor cancellation")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled processing did not finish")
			}
			f.assertNoText(t, d, "PROCESS_CANCELED")
			f.assertObject(t, d, content)
		})
	})
	t.Run("P08_real_PostgreSQL_claim_lock_deadline_HTTP_504_no_publication", func(t *testing.T) {
		content := []byte("database lock deadline fixture")
		d := f.upload(t, f.uploadToken, f.org, "claim-timeout.md", content)
		lock, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal("cannot begin synthetic row lock")
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = lock.Rollback(ctx)
		}()
		if _, err = lock.Exec(f.ctx, "SELECT id FROM knowledge_documents WHERE org_id=$1 AND id=$2 FOR UPDATE", f.org, d.ID); err != nil {
			t.Fatal("cannot hold real document row lock")
		}
		limits := f.limits
		limits.ProcessTimeout = 300 * time.Millisecond
		router := f.routerFor(f.newService(knowledge.NewTextExtractor(5<<20, 1<<20), limits))
		knowledgeExpect(t, knowledgeCall(router, f.uploadToken, "POST", f.path(f.org, d.ID)+"/process", nil, ""), 504, "PROCESS_TIMEOUT")
		if lock.Rollback(f.ctx) != nil {
			t.Fatal("cannot release synthetic document row lock")
		}
		// Claim errors can have an unknown server outcome after cancellation;
		// they must not publish text or blindly overwrite state with FAILED.
		f.assertNoText(t, d, "")
		f.assertObject(t, d, content)
	})
	t.Run("P08_real_PostgreSQL_text_INSERT_deadline_rolls_back_READY_and_text", func(t *testing.T) {
		content := []byte("database finalization deadline fixture")
		d := f.upload(t, f.uploadToken, f.org, "finalize-timeout.md", content)
		f.exec(t, `CREATE FUNCTION nexusdesk_w05_text_slow() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(2); RETURN NEW; END $$`)
		f.exec(t, `CREATE TRIGGER nexusdesk_w05_slow BEFORE INSERT ON knowledge_document_texts FOR EACH ROW EXECUTE FUNCTION nexusdesk_w05_text_slow()`)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := f.pool.Exec(ctx, `DROP TRIGGER nexusdesk_w05_slow ON knowledge_document_texts`); err != nil {
				t.Error("cannot remove slow text fixture trigger")
			}
			if _, err := f.pool.Exec(ctx, `DROP FUNCTION nexusdesk_w05_text_slow()`); err != nil {
				t.Error("cannot remove slow text fixture function")
			}
		})
		limits := f.limits
		limits.ProcessTimeout = 300 * time.Millisecond
		router := f.routerFor(f.newService(knowledge.NewTextExtractor(5<<20, 1<<20), limits))
		knowledgeExpect(t, knowledgeCall(router, f.uploadToken, "POST", f.path(f.org, d.ID)+"/process", nil, ""), 504, "PROCESS_TIMEOUT")
		f.assertNoText(t, d, "PROCESS_TIMEOUT")
		f.assertObject(t, d, content)
	})
	t.Run("missing_or_tampered_source_safe_500_and_no_text", func(t *testing.T) {
		for _, tamper := range []bool{false, true} {
			t.Run(fmt.Sprintf("tamper_%v", tamper), func(t *testing.T) {
				d := f.upload(t, f.uploadToken, f.org, "source.txt", []byte("original fixture"))
				path := filepath.Join(f.dir, f.key(t, d))
				code := "SOURCE_UNAVAILABLE"
				if tamper {
					// Same-length edit proves processing checks SHA, rather than only size.
					if os.WriteFile(path, []byte("modified fixture"), 0600) != nil {
						t.Fatal("cannot tamper synthetic fixture")
					}
					code = "SOURCE_CHANGED"
				} else if os.Remove(path) != nil {
					t.Fatal("cannot remove synthetic fixture")
				}
				if !tamper {
					knowledgeExpect(t, f.call(f.uploadToken, "GET", f.path(f.org, d.ID)+"/download"), 500, "INTERNAL_ERROR")
				}
				knowledgeExpect(t, f.call(f.uploadToken, "POST", f.path(f.org, d.ID)+"/process"), 500, "INTERNAL_ERROR")
				f.assertNoText(t, d, code)
			})
		}
	})
	t.Run("changed_source_size_download_safe_500", func(t *testing.T) {
		d := f.upload(t, f.uploadToken, f.org, "source-size.txt", []byte("source size fixture"))
		if os.WriteFile(filepath.Join(f.dir, f.key(t, d)), []byte("short"), 0600) != nil {
			t.Fatal("cannot change synthetic source size")
		}
		knowledgeExpect(t, f.call(f.uploadToken, "GET", f.path(f.org, d.ID)+"/download"), 500, "INTERNAL_ERROR")
	})
}

func knowledgeDocumentOperations() []struct{ method, suffix string } {
	return []struct{ method, suffix string }{{"GET", ""}, {"GET", "/download"}, {"GET", "/text"}, {"POST", "/process"}, {"DELETE", ""}}
}

func knowledgeFileCount(t *testing.T, directory string) int {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal("cannot inspect private fixture directory")
	}
	return len(entries)
}

// The wrapper only controls scheduling; successful extraction uses real code.
type knowledgeBlockingExtractor struct {
	entered                chan struct{}
	release                chan struct{}
	enterOnce, releaseOnce sync.Once
}

func newKnowledgeBlockingExtractor() *knowledgeBlockingExtractor {
	return &knowledgeBlockingExtractor{entered: make(chan struct{}), release: make(chan struct{})}
}
func (x *knowledgeBlockingExtractor) unblock() { x.releaseOnce.Do(func() { close(x.release) }) }
func (x *knowledgeBlockingExtractor) Extract(ctx context.Context, r io.Reader, media string) (knowledge.ExtractedText, error) {
	x.enterOnce.Do(func() { close(x.entered) })
	select {
	case <-ctx.Done():
		return knowledge.ExtractedText{}, ctx.Err()
	case <-x.release:
		return knowledge.NewTextExtractor(5<<20, 1<<20).Extract(ctx, r, media)
	}
}
func knowledgeWaitEntered(t *testing.T, x *knowledgeBlockingExtractor) {
	t.Helper()
	select {
	case <-x.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real ClaimProcess did not reach extractor")
	}
}
func knowledgeWaitResponse(t *testing.T, result <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case rec := <-result:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatal("processing response timed out")
		return nil
	}
}

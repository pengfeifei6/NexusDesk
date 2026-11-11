package knowledge

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service} }
func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	b := g.Group("/organizations/:orgID/knowledge/documents")
	b.POST("", h.upload)
	b.GET("", h.list)
	b.GET("/:documentID", h.get)
	b.POST("/:documentID/process", h.process)
	b.GET("/:documentID/text", h.text)
	b.GET("/:documentID/download", h.download)
	b.DELETE("/:documentID", h.remove)
}
func failure(c *gin.Context, err error) {
	status, code, message := 500, "INTERNAL_ERROR", "internal server error"
	var size *http.MaxBytesError
	var extraction *ExtractionError
	switch {
	case errors.Is(err, ErrInvalidRequest):
		status, code, message = 400, "INVALID_REQUEST", "invalid request"
	case errors.Is(err, ErrUnauthenticated):
		status, code, message = 401, "UNAUTHENTICATED", "authentication required"
		c.Header("WWW-Authenticate", "Bearer")
	case errors.Is(err, ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "permission denied"
	case errors.Is(err, ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "resource not found"
	case errors.Is(err, ErrConflict):
		status, code, message = 409, "DOCUMENT_STATE_CONFLICT", "document state conflict"
	case errors.Is(err, ErrTooLarge) || errors.As(err, &size):
		status, code, message = 413, "DOCUMENT_TOO_LARGE", "document exceeds size limit"
	case errors.Is(err, ErrUnsupported):
		status, code, message = 415, "UNSUPPORTED_DOCUMENT_TYPE", "unsupported document type"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = 504, "PROCESS_TIMEOUT", "document processing timed out"
	case errors.As(err, &extraction):
		switch extraction.Code {
		case "INVALID_TEXT", "EMPTY_TEXT", "TEXT_TOO_LARGE", "PDF_UNSUPPORTED":
			status, code, message = 422, extraction.Code, "document text extraction failed"
		}
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": ""}})
}
func positive(value string) (int64, error) {
	if value == "" {
		return 0, ErrInvalidRequest
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, ErrInvalidRequest
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, ErrInvalidRequest
	}
	return n, nil
}
func (h *Handler) scope(c *gin.Context) (actor, org, id int64, ok bool) {
	c.Header("Cache-Control", "no-store")
	actor, ok = auth.CurrentUserID(c)
	if !ok {
		failure(c, ErrUnauthenticated)
		return
	}
	var err error
	org, err = positive(c.Param("orgID"))
	if err != nil {
		failure(c, err)
		return actor, org, id, false
	}
	if err = h.service.Authorize(c.Request.Context(), actor, org); err != nil {
		failure(c, err)
		return actor, org, id, false
	}
	if raw := c.Param("documentID"); raw != "" {
		id, err = positive(raw)
		if err != nil {
			failure(c, err)
			return actor, org, id, false
		}
	}
	return actor, org, id, true
}
func noInput(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" || !auth.EmptyGET(c) {
		failure(c, ErrInvalidRequest)
		return false
	}
	return true
}

// EOF is only reported after validating that the entire multipart request has
// exactly one part. This prevents committing the first file of an invalid body.
type singlePartReader struct {
	part  *multipart.Part
	multi *multipart.Reader
	body  io.Reader
	done  bool
}

func (r *singlePartReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.part.Read(p)
	if err == io.EOF {
		r.done = true
		_, next := r.multi.NextRawPart()
		if next == nil {
			return n, ErrInvalidRequest
		}
		if next != io.EOF {
			return n, multipartError(next)
		}
		if _, e := io.Copy(io.Discard, r.body); e != nil {
			return n, multipartError(e)
		}
		return n, io.EOF
	}
	if err != nil {
		return n, multipartError(err)
	}
	return n, nil
}
func multipartError(err error) error {
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		return err
	}
	return ErrInvalidRequest
}
func (h *Handler) upload(c *gin.Context) {
	actor, org, _, ok := h.scope(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		failure(c, ErrInvalidRequest)
		return
	}
	if c.Request.ContentLength > h.service.limits.MaxRequestBytes {
		failure(c, ErrTooLarge)
		return
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		failure(c, ErrUnsupported)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.service.limits.MaxRequestBytes)
	defer c.Request.Body.Close()
	multi, err := c.Request.MultipartReader()
	if err != nil {
		failure(c, ErrInvalidRequest)
		return
	}
	part, err := multi.NextRawPart()
	if err != nil {
		failure(c, multipartError(err))
		return
	}
	kind, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if len(part.Header.Values("Content-Transfer-Encoding")) != 0 || err != nil || kind != "form-data" || params["name"] != "file" || params["filename"] == "" || len(params) != 2 {
		failure(c, ErrInvalidRequest)
		return
	}
	d, err := h.service.Upload(c.Request.Context(), actor, org, params["filename"], &singlePartReader{part: part, multi: multi, body: c.Request.Body})
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, gin.H{"data": d})
}
func (h *Handler) list(c *gin.Context) {
	actor, org, _, ok := h.scope(c)
	if !ok {
		return
	}
	if !auth.EmptyGET(c) {
		failure(c, ErrInvalidRequest)
		return
	}
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		failure(c, ErrInvalidRequest)
		return
	}
	f := Filter{Page: 1, PageSize: 20}
	for k, v := range values {
		if len(v) != 1 || v[0] == "" {
			failure(c, ErrInvalidRequest)
			return
		}
		switch k {
		case "page":
			f.Page, err = positive(v[0])
		case "page_size":
			f.PageSize, err = positive(v[0])
		case "status":
			f.Status = Status(v[0])
		default:
			err = ErrInvalidRequest
		}
		if err != nil {
			failure(c, err)
			return
		}
	}
	items, err := h.service.List(c.Request.Context(), actor, org, f)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"items": items, "page": f.Page, "page_size": f.PageSize}})
}
func (h *Handler) get(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	d, err := h.service.Get(c.Request.Context(), actor, org, id)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": d})
}
func (h *Handler) process(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	d, err := h.service.Process(c.Request.Context(), actor, org, id)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": d})
}
func (h *Handler) text(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	d, err := h.service.Text(c.Request.Context(), actor, org, id)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": d})
}
func (h *Handler) remove(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	if err := h.service.Delete(c.Request.Context(), actor, org, id); err != nil {
		failure(c, err)
		return
	}
	c.Status(204)
}
func (h *Handler) download(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	d, reader, err := h.service.Download(c.Request.Context(), actor, org, id)
	if err != nil {
		failure(c, err)
		return
	}
	defer reader.Close()
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": d.OriginalFilename})
	if disposition == "" {
		failure(c, ErrStorage)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", disposition)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Length", strconv.FormatInt(d.SizeBytes, 10))
	c.Status(200)
	if _, err = io.CopyN(c.Writer, reader, d.SizeBytes); err != nil {
		slog.Error("document download interrupted", "org_id", org, "document_id", id)
		c.Abort()
	}
}

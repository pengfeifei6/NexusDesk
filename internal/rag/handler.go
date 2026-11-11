package rag

import (
	"encoding/json"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	b := g.Group("/organizations/:orgID/knowledge")
	b.POST("/documents/:documentID/index", h.index)
	b.GET("/documents/:documentID/index", h.status)
	b.POST("/search", h.search)
	b.POST("/ask", h.ask)
}
func failure(c *gin.Context, err error) {
	code := publicCode(err)
	status := 500
	switch code {
	case "INVALID_REQUEST":
		status = 400
	case "UNAUTHENTICATED":
		status = 401
		c.Header("WWW-Authenticate", "Bearer")
	case "FORBIDDEN":
		status = 403
	case "NOT_FOUND":
		status = 404
	case "INDEX_STATE_CONFLICT", "INDEX_SOURCE_CHANGED":
		status = 409
	case "INDEX_CONTENT_INVALID", "INDEX_TOO_MANY_CHUNKS":
		status = 422
	case "EMBEDDING_COUNT_MISMATCH", "EMBEDDING_DIMENSION_MISMATCH", "EMBEDDING_INVALID_VECTOR", "MODEL_INVALID_RESPONSE", "MODEL_UPSTREAM_ERROR":
		status = 502
	case "MODEL_UNAVAILABLE", "MODEL_RATE_LIMITED", "RAG_BUSY":
		status = 503
	case "MODEL_TIMEOUT", "INDEX_TIMEOUT":
		status = 504
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": errorMessages[code], "request_id": ""}})
}
func positive(raw string) (int64, error) {
	if raw == "" {
		return 0, NewError("INVALID_REQUEST")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, NewError("INVALID_REQUEST")
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, NewError("INVALID_REQUEST")
	}
	return n, nil
}
func (h *Handler) scope(c *gin.Context) (actor, org, id int64, ok bool) {
	c.Header("Cache-Control", "no-store")
	actor, ok = auth.CurrentUserID(c)
	if !ok {
		failure(c, NewError("UNAUTHENTICATED"))
		return
	}
	var err error
	org, err = positive(c.Param("orgID"))
	if err != nil {
		failure(c, err)
		return actor, org, 0, false
	}
	if err = h.service.access.Authorize(c.Request.Context(), actor, org); err != nil {
		failure(c, err)
		return actor, org, 0, false
	}
	if raw := c.Param("documentID"); raw != "" {
		id, err = positive(raw)
		if err != nil {
			failure(c, err)
			return actor, org, 0, false
		}
	}
	return actor, org, id, true
}
func noInput(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" || !auth.EmptyGET(c) {
		failure(c, NewError("INVALID_REQUEST"))
		return false
	}
	return true
}
func readQuery(c *gin.Context) (QueryInput, bool) {
	if c.Request.URL.RawQuery != "" {
		failure(c, NewError("INVALID_REQUEST"))
		return QueryInput{}, false
	}
	fields, ok := auth.ReadJSONFields(c, 16*1024, []string{"query", "top_k"}, []string{"query"})
	if !ok {
		return QueryInput{}, false
	}
	var query *string
	if err := json.Unmarshal(fields["query"], &query); err != nil || query == nil {
		failure(c, NewError("INVALID_REQUEST"))
		return QueryInput{}, false
	}
	input := QueryInput{Query: *query, TopK: 5}
	if raw, present := fields["top_k"]; present {
		var k *int
		if err := json.Unmarshal(raw, &k); err != nil || k == nil || *k < 1 || *k > 10 {
			failure(c, NewError("INVALID_REQUEST"))
			return QueryInput{}, false
		}
		input.TopK = *k
	}
	input, err := normalizeQuery(input)
	if err != nil {
		failure(c, err)
		return QueryInput{}, false
	}
	return input, true
}
func (h *Handler) index(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	result, err := h.service.Index(c.Request.Context(), actor, org, id)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *Handler) status(c *gin.Context) {
	actor, org, id, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	result, err := h.service.IndexStatus(c.Request.Context(), actor, org, id)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *Handler) search(c *gin.Context) {
	actor, org, _, ok := h.scope(c)
	if !ok {
		return
	}
	input, ok := readQuery(c)
	if !ok {
		return
	}
	result, err := h.service.Search(c.Request.Context(), actor, org, input)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *Handler) ask(c *gin.Context) {
	actor, org, _, ok := h.scope(c)
	if !ok {
		return
	}
	input, ok := readQuery(c)
	if !ok {
		return
	}
	result, err := h.service.Ask(c.Request.Context(), actor, org, input)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

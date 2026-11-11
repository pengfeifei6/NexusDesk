package jobs

import (
	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service} }
func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	b := g.Group("/organizations/:orgID")
	b.POST("/knowledge/documents/:documentID/jobs", h.submit)
	b.GET("/knowledge/documents/:documentID/jobs", h.list)
	b.GET("/jobs/:jobID", h.get)
	b.POST("/jobs/:jobID/retry", h.retry)
	b.POST("/jobs/:jobID/cancel", h.cancel)
}
func failure(c *gin.Context, e error) {
	code := Code(e)
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
	case "JOB_STATE_CONFLICT", "DOCUMENT_STATE_CONFLICT":
		status = 409
	case "ASYNC_DISABLED":
		status = 503
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": newError(code).Error(), "request_id": ""}})
}
func (h *Handler) scope(c *gin.Context) (actor, org, id int64, ok bool) {
	c.Header("Cache-Control", "no-store")
	actor, ok = auth.CurrentUserID(c)
	if !ok {
		failure(c, newError("UNAUTHENTICATED"))
		return
	}
	var e error
	org, e = positive(c.Param("orgID"))
	if e != nil {
		failure(c, e)
		return actor, org, 0, false
	}
	if e = h.service.authorize(c.Request.Context(), actor, org); e != nil {
		failure(c, e)
		return actor, org, 0, false
	}
	raw := c.Param("documentID")
	if raw == "" {
		raw = c.Param("jobID")
	}
	id, e = positive(raw)
	if e != nil {
		failure(c, e)
		return actor, org, id, false
	}
	if c.Request.URL.RawQuery != "" || !auth.EmptyGET(c) {
		failure(c, newError("INVALID_REQUEST"))
		return actor, org, id, false
	}
	return actor, org, id, true
}
func (h *Handler) submit(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	j, e := h.service.Submit(c.Request.Context(), a, o, id)
	if e != nil {
		failure(c, e)
		return
	}
	c.JSON(202, gin.H{"data": j})
}
func (h *Handler) list(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	j, e := h.service.List(c.Request.Context(), a, o, id)
	if e != nil {
		failure(c, e)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"items": j}})
}
func (h *Handler) get(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	j, e := h.service.Get(c.Request.Context(), a, o, id)
	if e != nil {
		failure(c, e)
		return
	}
	c.JSON(200, gin.H{"data": j})
}
func (h *Handler) retry(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	j, e := h.service.Retry(c.Request.Context(), a, o, id)
	if e != nil {
		failure(c, e)
		return
	}
	c.JSON(200, gin.H{"data": j})
}
func (h *Handler) cancel(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	j, e := h.service.Cancel(c.Request.Context(), a, o, id)
	if e != nil {
		failure(c, e)
		return
	}
	c.JSON(200, gin.H{"data": j})
}

package organization

import (
	"errors"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

// RegisterRoutes must be mounted on a RequireUser-protected group.
func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/organizations", h.create)
	g.GET("/organizations", h.list)
}
func failure(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": observe.RequestID(c.Request.Context())}})
}
func domainFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated), errors.Is(err, ErrUserNotFound):
		c.Header("WWW-Authenticate", "Bearer")
		failure(c, 401, "UNAUTHENTICATED", "authentication required")
	case errors.Is(err, ErrInvalidRequest):
		failure(c, 400, "INVALID_REQUEST", "invalid request")
	default:
		failure(c, 500, "INTERNAL_ERROR", "internal server error")
	}
}
func identity(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	id, ok := auth.CurrentUserID(c)
	if !ok {
		domainFailure(c, ErrUnauthenticated)
	}
	return id, ok
}
func publicOrganization(v OrganizationView) gin.H {
	return gin.H{"id": v.Organization.ID, "name": v.Organization.Name, "my_role": v.MyRole}
}
func (h *Handler) create(c *gin.Context) {
	id, ok := identity(c)
	if !ok {
		return
	}
	fields, ok := auth.ReadStringFields(c, "name")
	if !ok {
		return
	}
	result, err := h.service.Create(c.Request.Context(), id, fields["name"])
	if err != nil {
		domainFailure(c, err)
		return
	}
	c.JSON(201, gin.H{"data": publicOrganization(result)})
}
func parsePagination(raw string) (int64, int64, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return 0, 0, ErrInvalidRequest
	}
	page, size := int64(1), int64(20)
	for key, entries := range values {
		if (key != "page" && key != "page_size") || len(entries) != 1 || entries[0] == "" {
			return 0, 0, ErrInvalidRequest
		}
		for _, b := range entries[0] {
			if b < '0' || b > '9' {
				return 0, 0, ErrInvalidRequest
			}
		}
		n, err := strconv.ParseInt(entries[0], 10, 64)
		if err != nil {
			return 0, 0, ErrInvalidRequest
		}
		if key == "page" {
			page = n
		} else {
			size = n
		}
	}
	_, err = pageQuery(page, size)
	return page, size, err
}
func (h *Handler) list(c *gin.Context) {
	id, ok := identity(c)
	if !ok {
		return
	}
	if !auth.EmptyGET(c) {
		domainFailure(c, ErrInvalidRequest)
		return
	}
	page, size, err := parsePagination(c.Request.URL.RawQuery)
	if err != nil {
		domainFailure(c, err)
		return
	}
	items, err := h.service.List(c.Request.Context(), id, page, size)
	if err != nil {
		domainFailure(c, err)
		return
	}
	dto := make([]gin.H, 0, len(items))
	for _, item := range items {
		dto = append(dto, publicOrganization(item))
	}
	c.JSON(200, gin.H{"data": gin.H{"items": dto, "page": page, "page_size": size}})
}

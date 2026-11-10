package organization

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
)

type MemberHandler struct{ service *MemberService }

func NewMemberHandler(s *MemberService) *MemberHandler { return &MemberHandler{service: s} }
func (h *MemberHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/organizations/:orgID/members", h.list)
	g.POST("/organizations/:orgID/members", h.add)
}
func memberFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		failure(c, 404, "NOT_FOUND", "resource not found")
	case errors.Is(err, ErrUserNotFound):
		failure(c, 404, "USER_NOT_FOUND", "user not found")
	case errors.Is(err, ErrForbidden):
		failure(c, 403, "FORBIDDEN", "permission denied")
	case errors.Is(err, ErrMemberExists):
		failure(c, 409, "MEMBER_ALREADY_EXISTS", "membership already exists")
	default:
		domainFailure(c, err)
	}
}
func (h *MemberHandler) authorized(c *gin.Context, write bool) (int64, int64, bool) {
	actor, ok := identity(c)
	if !ok {
		return 0, 0, false
	}
	raw := c.Param("orgID")
	if raw == "" {
		memberFailure(c, ErrInvalidRequest)
		return 0, 0, false
	}
	for _, b := range raw {
		if b < '0' || b > '9' {
			memberFailure(c, ErrInvalidRequest)
			return 0, 0, false
		}
	}
	org, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || org <= 0 {
		memberFailure(c, ErrInvalidRequest)
		return 0, 0, false
	}
	if err := h.service.Authorize(c.Request.Context(), actor, org, write); err != nil {
		memberFailure(c, err)
		return 0, 0, false
	}
	return actor, org, true
}
func publicMember(m Member) gin.H {
	return gin.H{"user_id": m.UserID, "display_name": m.DisplayName, "role": m.Role, "status": m.Status, "created_at": m.CreatedAt.UTC().Format(time.RFC3339)}
}
func (h *MemberHandler) list(c *gin.Context) {
	actor, org, ok := h.authorized(c, false)
	if !ok {
		return
	}
	if !auth.EmptyGET(c) {
		memberFailure(c, ErrInvalidRequest)
		return
	}
	page, size, err := parsePagination(c.Request.URL.RawQuery)
	if err != nil {
		memberFailure(c, err)
		return
	}
	items, err := h.service.List(c.Request.Context(), actor, org, page, size)
	if err != nil {
		memberFailure(c, err)
		return
	}
	dto := make([]gin.H, 0, len(items))
	for _, item := range items {
		dto = append(dto, publicMember(item))
	}
	c.JSON(200, gin.H{"data": gin.H{"items": dto, "page": page, "page_size": size}})
}
func (h *MemberHandler) add(c *gin.Context) {
	actor, org, ok := h.authorized(c, true)
	if !ok {
		return
	}
	fields, ok := auth.ReadObjectFields(c, "user_id", "role")
	if !ok {
		return
	}
	target, err := strconv.ParseInt(string(fields["user_id"]), 10, 64)
	var role Role
	if err != nil || target <= 0 || json.Unmarshal(fields["role"], &role) != nil || (role != RoleMember && role != RoleAdmin) {
		memberFailure(c, ErrInvalidRequest)
		return
	}
	member, err := h.service.Add(c.Request.Context(), actor, org, target, role)
	if err != nil {
		memberFailure(c, err)
		return
	}
	c.JSON(201, gin.H{"data": publicMember(member)})
}

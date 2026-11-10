package auth

import (
	"errors"
	"io"

	"github.com/gin-gonic/gin"
)

type MeHandler struct{ users UserFinder }

func NewMeHandler(users UserFinder) *MeHandler         { return &MeHandler{users: users} }
func (h *MeHandler) RegisterRoutes(g *gin.RouterGroup) { g.GET("/me", h.me) }

// EmptyGET rejects even chunked bodies without reading unbounded input.
func EmptyGET(c *gin.Context) bool {
	if c.Request.Body == nil {
		return true
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	return err == nil && len(data) == 0
}

func (h *MeHandler) me(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := CurrentUserID(c)
	if !ok {
		unauthenticated(c)
		return
	}
	if c.Request.URL.RawQuery != "" || !EmptyGET(c) {
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
		return
	}
	user, err := h.users.FindUserByID(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		unauthenticated(c)
		return
	}
	if err != nil || user.ID != id {
		apiError(c, 500, "INTERNAL_ERROR", "internal server error")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"id": user.ID, "email": user.Email, "display_name": user.DisplayName}})
}

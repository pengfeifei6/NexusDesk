package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

type TokenVerifier interface{ Verify(string) (int64, error) }
type UserFinder interface {
	FindUserByID(context.Context, int64) (User, error)
}

const currentUserIDKey = "nexusdesk.auth.user_id"

func CurrentUserID(c *gin.Context) (int64, bool) {
	value, exists := c.Get(currentUserIDKey)
	id, ok := value.(int64)
	return id, exists && ok && id > 0
}

func unauthenticated(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	apiError(c, 401, "UNAUTHENTICATED", "authentication required")
	c.Abort()
}

// RequireUser stores only a verified, currently existing user's ID.
func RequireUser(tokens TokenVerifier, users UserFinder) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		headers := c.Request.Header.Values("Authorization")
		if len(headers) != 1 || len(headers[0]) > maxAccessTokenBytes+7 {
			unauthenticated(c)
			return
		}
		scheme, raw, ok := strings.Cut(headers[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" || strings.ContainsAny(raw, " \t\r\n,") {
			unauthenticated(c)
			return
		}
		if c.Request.Context().Err() != nil {
			apiError(c, 500, "INTERNAL_ERROR", "internal server error")
			c.Abort()
			return
		}
		id, err := tokens.Verify(raw)
		if err != nil || id <= 0 {
			unauthenticated(c)
			return
		}
		user, err := users.FindUserByID(c.Request.Context(), id)
		if errors.Is(err, ErrNotFound) {
			unauthenticated(c)
			return
		}
		if err != nil || user.ID != id || c.Request.Context().Err() != nil {
			apiError(c, 500, "INTERNAL_ERROR", "internal server error")
			c.Abort()
			return
		}
		c.Set(currentUserIDKey, id)
		c.Next()
	}
}

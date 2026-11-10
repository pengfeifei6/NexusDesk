package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

// Pins A08 at the protected HTTP boundary as well as TokenManager.Verify.
func TestProtectedHTTPRejectsInvalidAlgorithmsAndClaims(t *testing.T) {
	manager := fixtureTokenManager(t)
	router := httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.GET("/protected", RequireUser(manager, userFinderFunc(func(context.Context, int64) (User, error) { return User{}, ErrStorage })), func(c *gin.Context) { c.Status(204) })
	})
	for _, name := range []string{"HS384", "none", "wrong_key", "issuer", "audience", "sub", "iat", "exp", "expired"} {
		t.Run(name, func(t *testing.T) {
			now := fixtureTokenTime.Unix()
			claims := jwt.MapClaims{"sub": "42", "iss": "nexusdesk", "aud": []string{"nexusdesk-api"}, "iat": now, "exp": now + 900}
			method := jwt.SigningMethod(jwt.SigningMethodHS256)
			var key any = fixtureTokenConfig().Secret
			switch name {
			case "HS384":
				method = jwt.SigningMethodHS384
			case "none":
				method = jwt.SigningMethodNone
				key = jwt.UnsafeAllowNoneSignatureType
			case "wrong_key":
				key = []byte("Other-Only-Test-Fixture-Key-32-Bytes")
			case "issuer":
				claims["iss"] = "other"
			case "audience":
				claims["aud"] = []string{"other"}
			case "sub", "iat", "exp":
				delete(claims, name)
			case "expired":
				claims["iat"] = now - 901
				claims["exp"] = now - 1
			}
			raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
			if err != nil {
				t.Fatal("token fixture signing failed")
			}
			req := httptest.NewRequest("GET", "/api/v1/protected", nil)
			req.Header.Set("Authorization", "Bearer "+raw)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != "Bearer" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("invalid credential HTTP status %d", rec.Code)
			}
		})
	}
}

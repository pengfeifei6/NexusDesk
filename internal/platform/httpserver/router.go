// Package httpserver provides the API's HTTP routing.
package httpserver

import (
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
)

// NewRouter builds the minimal API. Health does not depend on the database.
func NewRouter(registerRoutes ...func(*gin.RouterGroup)) *gin.Engine {
	return NewRouterWithObserver(observe.NewRegistry(), slog.Default(), registerRoutes...)
}

// NewRouterWithObserver wires safe request correlation, tracing and telemetry.
func NewRouterWithObserver(registry *observe.Registry, logger *slog.Logger, registerRoutes ...func(*gin.RouterGroup)) *gin.Engine {
	router := gin.New()
	// Gin's built-in redirects run before middleware. Explicit redirect routes
	// keep them inside the same request lifecycle without changing the writer.
	router.RedirectTrailingSlash = false
	aliases := make(map[string]string)
	router.Use(observation(registry, logger, aliases))
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	api := router.Group("/api/v1")
	for _, register := range registerRoutes {
		register(api)
	}
	canonical := router.Routes()
	routes := make([]string, 0, len(canonical))
	existing := make(map[string]bool, len(canonical))
	for _, route := range canonical {
		existing[route.Method+" "+route.Path] = true
	}
	for _, route := range canonical {
		routes = append(routes, route.Path)
	}
	registry.RegisterRoutes(routes)
	for _, route := range canonical {
		if route.Method == http.MethodConnect || route.Path == "/" {
			continue
		}
		alias := route.Path + "/"
		if strings.HasSuffix(route.Path, "/") {
			alias = strings.TrimSuffix(route.Path, "/")
		}
		// A catch-all already matches every suffix; only its missing slash at the
		// fixed prefix needs an alias (e.g. /files -> /files/*path).
		if wildcard := strings.Index(route.Path, "/*"); wildcard >= 0 {
			alias = route.Path[:wildcard]
		}
		if alias == "" || existing[route.Method+" "+alias] {
			continue
		}
		aliases[route.Method+" "+alias] = route.Path
		router.Handle(route.Method, alias, redirectSlash)
	}
	return router
}

// redirectSlash preserves the selected Gin version's Location and 301/307
// behavior, including query strings and its sanitized forwarded prefix.
func redirectSlash(c *gin.Context) {
	req := c.Request
	target := req.URL.Path
	if prefix := path.Clean(req.Header.Get("X-Forwarded-Prefix")); prefix != "." {
		prefix = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '/' || r == '-' {
				return r
			}
			return -1
		}, prefix)
		for strings.Contains(prefix, "//") {
			prefix = strings.ReplaceAll(prefix, "//", "/")
		}
		target = prefix + "/" + target
	}
	req.URL.Path = target + "/"
	if len(target) > 1 && strings.HasSuffix(target, "/") {
		req.URL.Path = target[:len(target)-1]
	}
	status := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		status = http.StatusTemporaryRedirect
	}
	http.Redirect(c.Writer, req, req.URL.String(), status)
}

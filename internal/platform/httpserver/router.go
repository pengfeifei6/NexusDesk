// Package httpserver provides the API's HTTP routing.
package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the minimal API. Health does not depend on the database.
func NewRouter(registerRoutes ...func(*gin.RouterGroup)) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	api := router.Group("/api/v1")
	for _, register := range registerRoutes {
		register(api)
	}
	return router
}

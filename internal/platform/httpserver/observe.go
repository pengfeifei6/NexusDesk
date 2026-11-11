package httpserver

import (
	"log/slog"
	"net/http"
	"runtime/trace"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
)

func observation(registry *observe.Registry, logger *slog.Logger, aliases map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		ctx := observe.WithRequestID(c.Request.Context())
		id := observe.RequestID(ctx)
		ctx, task := trace.NewTask(ctx, "http.request")
		defer task.End()
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", id)
		registry.HTTPStarted()
		defer func() {
			panicked := recover() != nil
			if panicked {
				c.Abort()
				if !c.Writer.Written() {
					c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "internal server error", "request_id": id}})
				}
			}
			template := c.FullPath()
			if canonical, ok := aliases[c.Request.Method+" "+template]; ok {
				template = canonical
			}
			route := registry.Route(template)
			duration := time.Since(started)
			registry.HTTPCompleted(c.Request.Method, route, c.Writer.Status(), duration)
			trace.Log(ctx, "http.route", route)
			attrs := []any{"method", observe.Method(c.Request.Method), "route", route, "status", c.Writer.Status(), "duration_seconds", duration.Seconds(), "request_id", id}
			if panicked {
				logger.Error("HTTP panic recovered", attrs...)
			} else {
				logger.Info("HTTP request completed", attrs...)
			}
		}()
		// The original Gin writer is untouched, including its flush/cancel behavior.
		c.Next()
	}
}

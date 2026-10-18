package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// Logger writes one log line per request. It logs the route pattern, never
// the concrete URL, headers or body, so external IDs and credentials stay out
// of the logs.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		attrs := []slog.Attr{
			slog.String("request_id", c.GetString(problem.RequestIDKey)),
			slog.String("http.request.method", c.Request.Method),
			slog.String("http.route", c.FullPath()),
			slog.Int("http.response.status_code", status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		}
		if p, ok := auth.PrincipalFromContext(c.Request.Context()); ok {
			attrs = append(attrs,
				slog.String("project_id", p.ProjectID.String()),
				slog.String("key_id", p.KeyID.String()))
		}
		if code := c.GetString(problem.ErrorCodeKey); code != "" {
			attrs = append(attrs, slog.String("error.code", code))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("error", c.Errors.String()))
		}
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.LogAttrs(c.Request.Context(), level, "http request", attrs...)
	}
}

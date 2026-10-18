package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// Recover turns panics into a 500 problem response.
func Recover(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if err, ok := r.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(r)
			}
			ctx := c.Request.Context()
			logger.ErrorContext(ctx, "panic recovered", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			trace.SpanFromContext(ctx).RecordError(fmt.Errorf("panic: %v", r))
			if c.Writer.Written() {
				c.Abort()
				return
			}
			problem.Abort(c, http.StatusInternalServerError, api.ProblemCodeInternal, "")
		}()
		c.Next()
	}
}

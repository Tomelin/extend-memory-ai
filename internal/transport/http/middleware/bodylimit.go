package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// BodyLimit rejects bodies larger than maxBytes.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			problem.Abort(c, http.StatusRequestEntityTooLarge, api.ProblemCodePayloadTooLarge,
				fmt.Sprintf("o corpo deve ter no máximo %d bytes", maxBytes))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

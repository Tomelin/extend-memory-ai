// Package middleware contains the gin middlewares of the public API.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// HeaderRequestID carries the request identifier in both directions.
const HeaderRequestID = "X-Request-ID"

// RequestID accepts a well-formed X-Request-ID from the client or generates
// one, echoes it in the response and records it on the span.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !validRequestID(id) {
			id = uuid.Must(uuid.NewV7()).String()
		}
		c.Set(problem.RequestIDKey, id)
		c.Header(HeaderRequestID, id)
		trace.SpanFromContext(c.Request.Context()).SetAttributes(attribute.String("request.id", id))
		c.Next()
	}
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

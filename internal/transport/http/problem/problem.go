// Package problem writes RFC 9457 application/problem+json responses.
package problem

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
)

// ContentType is the media type of every error response.
const ContentType = "application/problem+json"

// Keys stored in the gin context.
const (
	RequestIDKey = "request_id"
	ErrorCodeKey = "error_code"
)

const typeBase = "https://extend-memory-ai/errors/"

var titles = map[api.ProblemCode]string{
	api.ProblemCodeMalformedRequest:  "Requisição malformada",
	api.ProblemCodeUnauthenticated:   "Credencial ausente ou inválida",
	api.ProblemCodeInsufficientScope: "Escopo insuficiente",
	api.ProblemCodeNotFound:          "Recurso não encontrado",
	api.ProblemCodeMethodNotAllowed:  "Método não permitido",
	api.ProblemCodePayloadTooLarge:   "Corpo acima do limite",
	api.ProblemCodeValidationFailed:  "Requisição inválida",
	api.ProblemCodeRateLimited:       "Limite de requisições excedido",
	api.ProblemCodeInternal:          "Erro interno",
	api.ProblemCodeUnavailable:       "Serviço indisponível",
}

// New builds a problem document.
func New(status int, code api.ProblemCode, detail string) api.Problem {
	title, ok := titles[code]
	if !ok {
		title = http.StatusText(status)
	}
	p := api.Problem{Type: typeBase + string(code), Title: title, Status: status, Code: code}
	if detail != "" {
		p.Detail = &detail
	}
	return p
}

// Abort writes the problem, stops the handler chain and records error.code
// on the request span (5xx also marks the span as failed).
func Abort(c *gin.Context, status int, code api.ProblemCode, detail string) {
	p := New(status, code, detail)
	if id := c.GetString(RequestIDKey); id != "" {
		p.RequestId = &id
	}
	c.Set(ErrorCodeKey, string(code))
	span := trace.SpanFromContext(c.Request.Context())
	span.SetAttributes(attribute.String("error.code", string(code)))
	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, string(code))
	}
	c.Header("Content-Type", ContentType)
	c.AbortWithStatusJSON(status, p)
}

// AbortWithError maps a domain error to the matching problem. Unexpected
// errors become 500 without exposing details.
func AbortWithError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		Abort(c, http.StatusNotFound, api.ProblemCodeNotFound, "")
	case errors.Is(err, domain.ErrValidation):
		Abort(c, http.StatusUnprocessableEntity, api.ProblemCodeValidationFailed, "")
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, domain.ErrRetryable):
		trace.SpanFromContext(c.Request.Context()).RecordError(err)
		Abort(c, http.StatusServiceUnavailable, api.ProblemCodeUnavailable, "")
	default:
		trace.SpanFromContext(c.Request.Context()).RecordError(err)
		_ = c.Error(err)
		Abort(c, http.StatusInternalServerError, api.ProblemCodeInternal, "")
	}
}

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
	api.ProblemCodeMalformedRequest:    "Requisição malformada",
	api.ProblemCodeUnauthenticated:     "Credencial ausente ou inválida",
	api.ProblemCodeInsufficientScope:   "Escopo insuficiente",
	api.ProblemCodeNotFound:            "Recurso não encontrado",
	api.ProblemCodeSpaceNotFound:       "Espaço não encontrado",
	api.ProblemCodeMemoryNotFound:      "Memória não encontrada",
	api.ProblemCodeIdempotencyConflict: "Conflito de idempotência",
	api.ProblemCodeLinkConflict:        "Relação já existente",
	api.ProblemCodeMethodNotAllowed:    "Método não permitido",
	api.ProblemCodePayloadTooLarge:     "Corpo acima do limite",
	api.ProblemCodeValidationFailed:    "Requisição inválida",
	api.ProblemCodeRateLimited:         "Limite de requisições excedido",
	api.ProblemCodeInternal:            "Erro interno",
	api.ProblemCodeUnavailable:         "Serviço indisponível",
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
	write(c, New(status, code, detail))
}

func write(c *gin.Context, p api.Problem) {
	status, code := p.Status, p.Code
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
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		abortValidation(c, ve)
	case errors.Is(err, domain.ErrSpaceNotFound):
		Abort(c, http.StatusNotFound, api.ProblemCodeSpaceNotFound, "")
	case errors.Is(err, domain.ErrMemoryNotFound):
		Abort(c, http.StatusNotFound, api.ProblemCodeMemoryNotFound, "")
	case errors.Is(err, domain.ErrIdempotencyConflict):
		Abort(c, http.StatusConflict, api.ProblemCodeIdempotencyConflict,
			"a chave de idempotência já foi usada com outro conteúdo")
	case errors.Is(err, domain.ErrLinkConflict):
		Abort(c, http.StatusConflict, api.ProblemCodeLinkConflict, "a relação já existe")
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

// abortValidation lists every invalid field in "errors"; detail repeats the
// first one for clients that only show detail.
func abortValidation(c *gin.Context, ve *domain.ValidationError) {
	detail := ""
	if len(ve.Fields) > 0 {
		detail = ve.Fields[0].Field + ": " + ve.Fields[0].Message
	}
	p := New(http.StatusUnprocessableEntity, api.ProblemCodeValidationFailed, detail)
	fields := make([]struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}, len(ve.Fields))
	for i, f := range ve.Fields {
		fields[i].Field, fields[i].Message = f.Field, f.Message
	}
	p.Errors = &fields
	write(c, p)
}

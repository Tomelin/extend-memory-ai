package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// Authenticator verifies bearer tokens.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (domain.Principal, error)
	RecordFailure(ctx context.Context, reason string)
}

// Authenticate requires "Authorization: Bearer emk_..." and stores the
// principal in the request context.
func Authenticate(a Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		header := c.GetHeader("Authorization")
		if header == "" {
			a.RecordFailure(ctx, auth.ReasonMissing)
			problem.Abort(c, http.StatusUnauthorized, api.ProblemCodeUnauthenticated, "cabeçalho Authorization ausente")
			return
		}
		scheme, token, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			a.RecordFailure(ctx, auth.ReasonInvalid)
			problem.Abort(c, http.StatusUnauthorized, api.ProblemCodeUnauthenticated, "use Authorization: Bearer <chave>")
			return
		}

		p, err := a.Authenticate(ctx, token)
		switch {
		case errors.Is(err, auth.ErrUnauthenticated):
			problem.Abort(c, http.StatusUnauthorized, api.ProblemCodeUnauthenticated, "chave de API inválida, expirada ou revogada")
			return
		case err != nil:
			problem.AbortWithError(c, err)
			return
		}

		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("project.id", p.ProjectID.String()),
			attribute.String("api_key.id", p.KeyID.String()),
		)
		c.Request = c.Request.WithContext(auth.WithPrincipal(ctx, p))
		c.Next()
	}
}

// RequireScope rejects principals without scope with 403.
func RequireScope(a Authenticator, scope domain.Scope) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := auth.PrincipalFromContext(c.Request.Context())
		if !ok || !p.HasScope(scope) {
			a.RecordFailure(c.Request.Context(), auth.ReasonScope)
			problem.Abort(c, http.StatusForbidden, api.ProblemCodeInsufficientScope, "escopo exigido: "+string(scope))
			return
		}
		c.Next()
	}
}

package auth

import (
	"context"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

type principalKey struct{}

// WithPrincipal stores the authenticated principal in ctx.
func WithPrincipal(ctx context.Context, p domain.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext returns the principal stored by WithPrincipal.
func PrincipalFromContext(ctx context.Context) (domain.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(domain.Principal)
	return p, ok
}

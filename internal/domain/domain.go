// Package domain holds the core types shared by the API layers.
package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// Scope is a permission granted to an API key.
type Scope string

const (
	ScopeMemoriesRead  Scope = "memories:read"
	ScopeMemoriesWrite Scope = "memories:write"
	ScopeSpacesWrite   Scope = "spaces:write"
)

// AllScopes lists every scope accepted by the service.
var AllScopes = []Scope{ScopeMemoriesRead, ScopeMemoriesWrite, ScopeSpacesWrite}

// ParseScopes parses a comma-separated list of scopes, rejecting unknown or
// duplicated values.
func ParseScopes(csv string) ([]Scope, error) {
	var scopes []Scope
	for raw := range strings.SplitSeq(csv, ",") {
		s := Scope(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if !slices.Contains(AllScopes, s) {
			return nil, fmt.Errorf("%w: unknown scope %q", ErrValidation, s)
		}
		if slices.Contains(scopes, s) {
			return nil, fmt.Errorf("%w: duplicated scope %q", ErrValidation, s)
		}
		scopes = append(scopes, s)
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("%w: at least one scope is required", ErrValidation)
	}
	return scopes, nil
}

// Principal is the authenticated caller. The project always comes from the
// credential, never from the request.
type Principal struct {
	ProjectID uuid.UUID
	KeyID     uuid.UUID
	Scopes    []Scope
}

// HasScope reports whether the principal was granted scope.
func (p Principal) HasScope(scope Scope) bool {
	return slices.Contains(p.Scopes, scope)
}

// Project is a tenant.
type Project struct {
	ID          uuid.UUID
	Slug        string
	Name        string
	Description *string
}

// APIKeyCredential is the stored data needed to verify a presented API key.
type APIKeyCredential struct {
	KeyID      uuid.UUID
	ProjectID  uuid.UUID
	SecretHash []byte
	Scopes     []Scope
}

var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrValidation  = errors.New("validation failed")
	ErrRetryable   = errors.New("retryable")
	ErrUnavailable = errors.New("unavailable")
)

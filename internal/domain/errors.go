package domain

import (
	"fmt"
	"strings"
)

// Specific errors keep the generic category reachable with errors.Is, so
// callers can map either the category or the precise cause.
var (
	ErrSpaceNotFound       = fmt.Errorf("space %w", ErrNotFound)
	ErrMemoryNotFound      = fmt.Errorf("memory %w", ErrNotFound)
	ErrIdempotencyConflict = fmt.Errorf("idempotency key reused with different content: %w", ErrConflict)
	ErrLinkConflict        = fmt.Errorf("link already exists: %w", ErrConflict)
	ErrLinkNotFound        = fmt.Errorf("link %w", ErrNotFound)
)

// FieldError describes why a single input field is invalid. Field uses the
// JSON path of the public contract (for example "embedding.vector").
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of an input. It matches
// ErrValidation with errors.Is.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return ErrValidation.Error() + ": " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrValidation) true.
func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation
}

// fieldErrors accumulates validation failures so that all of them are
// reported at once.
type fieldErrors struct {
	prefix string
	list   []FieldError
}

func (v *fieldErrors) add(field, format string, args ...any) {
	if v.prefix != "" {
		field = v.prefix + "." + field
	}
	v.list = append(v.list, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
}

func (v *fieldErrors) err() error {
	if len(v.list) == 0 {
		return nil
	}
	return &ValidationError{Fields: v.list}
}

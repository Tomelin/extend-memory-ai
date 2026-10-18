package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

func TestMapError(t *testing.T) {
	plain := errors.New("boom")
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"no rows", pgx.ErrNoRows, domain.ErrNotFound},
		{"unique", &pgconn.PgError{Code: "23505"}, domain.ErrConflict},
		{"fk", &pgconn.PgError{Code: "23503"}, domain.ErrValidation},
		{"check", &pgconn.PgError{Code: "23514"}, domain.ErrValidation},
		{"serialization", &pgconn.PgError{Code: "40001"}, domain.ErrRetryable},
		{"deadlock", fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40P01"}), domain.ErrRetryable},
		{"deadline", context.DeadlineExceeded, domain.ErrUnavailable},
		{"other", plain, plain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapError(tc.in)
			if !errors.Is(got, tc.want) {
				t.Errorf("MapError(%v) = %v, want %v", tc.in, got, tc.want)
			}
			if !errors.Is(got, tc.in) {
				t.Errorf("MapError lost the original error")
			}
		})
	}
	if MapError(nil) != nil {
		t.Error("MapError(nil) != nil")
	}
}

func TestQueryName(t *testing.T) {
	ctx := context.Background()
	if got := QueryName(ctx, "-- name: GetProject :one\nSELECT 1"); got != "GetProject" {
		t.Errorf("got %q, want GetProject", got)
	}
	if got := QueryName(ctx, "select 1"); got != "SELECT" {
		t.Errorf("got %q, want SELECT", got)
	}
}

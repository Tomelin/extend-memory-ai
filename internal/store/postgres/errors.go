package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

// MapError translates driver errors into domain errors, keeping the original
// error in the chain.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %w", domain.ErrNotFound, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %w", domain.ErrConflict, err)
		case "23503", "23514", "22P02":
			return fmt.Errorf("%w: %w", domain.ErrValidation, err)
		case "40001", "40P01":
			return fmt.Errorf("%w: %w", domain.ErrRetryable, err)
		case "57P01", "57P02", "57P03", "53300":
			return fmt.Errorf("%w: %w", domain.ErrUnavailable, err)
		}
		return err
	}
	var connErr *pgconn.ConnectError
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &connErr) || pgconn.Timeout(err) {
		return fmt.Errorf("%w: %w", domain.ErrUnavailable, err)
	}
	return err
}

// isClientError reports whether err is an expected domain outcome that must
// not mark spans as failed.
func isClientError(err error) bool {
	return errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, domain.ErrConflict) ||
		errors.Is(err, domain.ErrValidation)
}

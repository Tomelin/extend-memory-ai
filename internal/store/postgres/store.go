package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres/sqlcdb"
)

const instrumentationName = "github.com/Tomelin/extend-memory-ai/internal/store/postgres"

// Store is the persistence entry point of the API. It connects as
// memory_app, which is subject to RLS.
type Store struct {
	pool   *pgxpool.Pool
	tracer trace.Tracer
}

// New creates a Store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, tracer: otel.Tracer(instrumentationName)}
}

// WithProject runs fn in a transaction whose app.current_project_id is
// projectID, so RLS only exposes that project's rows. It is the only way to
// run domain queries.
func (s *Store) WithProject(ctx context.Context, projectID uuid.UUID, fn func(q *sqlcdb.Queries) error) (err error) {
	ctx, span := s.tracer.Start(ctx, "store.WithProject")
	defer func() {
		if err != nil && !isClientError(err) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "transaction failed")
		}
		span.End()
	}()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MapError(err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	q := sqlcdb.New(tx)
	if err = q.SetCurrentProject(ctx, projectID.String()); err != nil {
		return MapError(err)
	}
	if err = fn(q); err != nil {
		return MapError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return MapError(err)
	}
	return nil
}

// GetProject returns the project, visible only through its own tenant context.
func (s *Store) GetProject(ctx context.Context, projectID uuid.UUID) (domain.Project, error) {
	var p domain.Project
	err := s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		row, err := q.GetProject(ctx, projectID)
		if err != nil {
			return err
		}
		p = domain.Project{ID: row.ID, Slug: row.Slug, Name: row.Name, Description: row.Description}
		return nil
	})
	return p, err
}

// ResolveAPIKey returns the active key with the given prefix through the
// SECURITY DEFINER function resolve_api_key; memory_app cannot read the keys
// table directly.
func (s *Store) ResolveAPIKey(ctx context.Context, prefix string) (domain.APIKeyCredential, error) {
	row, err := sqlcdb.New(s.pool).ResolveAPIKey(ctx, prefix)
	if err != nil {
		return domain.APIKeyCredential{}, MapError(err)
	}
	scopes := make([]domain.Scope, len(row.Scopes))
	for i, sc := range row.Scopes {
		scopes[i] = domain.Scope(sc)
	}
	return domain.APIKeyCredential{
		KeyID:      row.ID,
		ProjectID:  row.ProjectID,
		SecretHash: row.SecretHash,
		Scopes:     scopes,
	}, nil
}

// TouchAPIKey updates last_used_at of the key.
func (s *Store) TouchAPIKey(ctx context.Context, keyID uuid.UUID) error {
	return MapError(sqlcdb.New(s.pool).TouchAPIKey(ctx, keyID))
}

// Ping checks that the database answers queries.
func (s *Store) Ping(ctx context.Context) error {
	_, err := sqlcdb.New(s.pool).Ping(ctx)
	return MapError(err)
}

// ErrSchemaMismatch is returned when the database does not match the binary.
var ErrSchemaMismatch = errors.New("database schema mismatch")

// CheckSchema verifies that the database is at expectedVersion, not dirty,
// and that memories.embedding has the configured number of dimensions.
func (s *Store) CheckSchema(ctx context.Context, expectedVersion uint, embeddingDimensions int) error {
	q := sqlcdb.New(s.pool)
	v, err := q.SchemaVersion(ctx)
	if err != nil {
		if errors.Is(MapError(err), domain.ErrNotFound) {
			return fmt.Errorf("%w: no migration applied, expected version %d", ErrSchemaMismatch, expectedVersion)
		}
		return fmt.Errorf("read schema version: %w", MapError(err))
	}
	if v.Dirty {
		return fmt.Errorf("%w: version %d is dirty", ErrSchemaMismatch, v.Version)
	}
	if v.Version != int64(expectedVersion) {
		return fmt.Errorf("%w: found version %d, expected %d", ErrSchemaMismatch, v.Version, expectedVersion)
	}
	dims, err := q.EmbeddingDimensions(ctx)
	if err != nil {
		return fmt.Errorf("read embedding dimensions: %w", MapError(err))
	}
	if int(dims) != embeddingDimensions {
		return fmt.Errorf("%w: memories.embedding has %d dimensions, configured %d", ErrSchemaMismatch, dims, embeddingDimensions)
	}
	return nil
}

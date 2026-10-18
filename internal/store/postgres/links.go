package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres/sqlcdb"
)

// CreateLink stores a relation. The target must be an active memory of the
// same space; otherwise the request is invalid (422), as the contract says.
func (s *Store) CreateLink(ctx context.Context, projectID uuid.UUID, externalID string, nl domain.NewLink) (l domain.Link, err error) {
	md, err := json.Marshal(nl.Metadata)
	if err != nil {
		return l, err
	}
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		if err := activeMemory(ctx, q, space.ID, nl.SourceMemoryID); err != nil {
			return err
		}
		if err := activeMemory(ctx, q, space.ID, nl.TargetMemoryID); err != nil {
			if errors.Is(err, domain.ErrMemoryNotFound) {
				return &domain.ValidationError{Fields: []domain.FieldError{{
					Field: "target_memory_id", Message: "deve ser uma memória ativa do mesmo espaço",
				}}}
			}
			return err
		}
		row, err := q.InsertLink(ctx, sqlcdb.InsertLinkParams{
			ProjectID: projectID, SpaceID: space.ID,
			SourceMemoryID: nl.SourceMemoryID, TargetMemoryID: nl.TargetMemoryID,
			RelationType: string(nl.RelationType), Weight: nl.Weight, Metadata: md,
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrLinkConflict
		}
		if err != nil {
			return err
		}
		l, err = toLink(row)
		return err
	})
	return l, err
}

// ListLinks returns the relations of an active memory whose other end is
// also active.
func (s *Store) ListLinks(ctx context.Context, projectID uuid.UUID, externalID string, memoryID uuid.UUID, f domain.LinkFilter) (links []domain.Link, err error) {
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		if err := activeMemory(ctx, q, space.ID, memoryID); err != nil {
			return err
		}
		rows, err := q.ListLinks(ctx, sqlcdb.ListLinksParams{
			SpaceID:      space.ID,
			MemoryID:     memoryID,
			Outgoing:     f.Direction != domain.LinkIncoming,
			Incoming:     f.Direction != domain.LinkOutgoing,
			RelationType: nonEmpty(string(f.RelationType)),
		})
		if err != nil {
			return err
		}
		links = make([]domain.Link, len(rows))
		for i, r := range rows {
			if links[i], err = toLink(r.MemoryLink); err != nil {
				return err
			}
		}
		return nil
	})
	return links, err
}

// LinksOf returns the direct relations of the given memories.
func (s *Store) LinksOf(ctx context.Context, projectID uuid.UUID, externalID string, memoryIDs []uuid.UUID) (links []domain.Link, err error) {
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		rows, err := q.ListLinksOfMemories(ctx, sqlcdb.ListLinksOfMemoriesParams{SpaceID: space.ID, MemoryIds: memoryIDs})
		if err != nil {
			return err
		}
		links = make([]domain.Link, len(rows))
		for i, r := range rows {
			if links[i], err = toLink(r.MemoryLink); err != nil {
				return err
			}
		}
		return nil
	})
	return links, err
}

// DeleteLink removes a relation or returns domain.ErrLinkNotFound.
func (s *Store) DeleteLink(ctx context.Context, projectID uuid.UUID, externalID string, source, target uuid.UUID, relation domain.RelationType) error {
	return s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		n, err := q.DeleteLink(ctx, sqlcdb.DeleteLinkParams{
			SpaceID: space.ID, SourceMemoryID: source, TargetMemoryID: target, RelationType: string(relation),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrLinkNotFound
		}
		return nil
	})
}

func activeMemory(ctx context.Context, q *sqlcdb.Queries, spaceID, id uuid.UUID) error {
	_, err := q.GetActiveMemory(ctx, sqlcdb.GetActiveMemoryParams{SpaceID: spaceID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrMemoryNotFound
	}
	return err
}

func toLink(r sqlcdb.MemoryLink) (domain.Link, error) {
	var md domain.Metadata
	if err := json.Unmarshal(r.Metadata, &md); err != nil {
		return domain.Link{}, fmt.Errorf("decode link metadata: %w", err)
	}
	return domain.Link{
		SourceMemoryID: r.SourceMemoryID, TargetMemoryID: r.TargetMemoryID,
		RelationType: domain.RelationType(r.RelationType), Weight: r.Weight,
		Metadata: md, CreatedAt: r.CreatedAt,
	}, nil
}

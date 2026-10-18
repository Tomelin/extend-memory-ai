package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgvector "github.com/pgvector/pgvector-go"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres/sqlcdb"
)

// UpsertSpace creates the space or updates name, kind and metadata. created
// reports whether it did not exist.
func (s *Store) UpsertSpace(ctx context.Context, projectID uuid.UUID, in domain.SpaceInput) (sp domain.Space, created bool, err error) {
	md, err := json.Marshal(in.Metadata)
	if err != nil {
		return domain.Space{}, false, err
	}
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		row, err := q.InsertSpace(ctx, sqlcdb.InsertSpaceParams{
			ProjectID: projectID, ExternalID: in.ExternalID, Name: in.Name, Kind: string(in.Kind), Metadata: md,
		})
		switch {
		case err == nil:
			created = true
		case errors.Is(err, pgx.ErrNoRows):
			row, err = q.UpdateSpace(ctx, sqlcdb.UpdateSpaceParams{
				ExternalID: in.ExternalID, Name: in.Name, Kind: string(in.Kind), Metadata: md,
			})
			if err != nil {
				return err
			}
		default:
			return err
		}
		sp, err = toSpace(row)
		return err
	})
	return sp, created, err
}

// GetSpace returns the space or domain.ErrSpaceNotFound.
func (s *Store) GetSpace(ctx context.Context, projectID uuid.UUID, externalID string) (sp domain.Space, err error) {
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		row, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		sp, err = toSpace(row)
		return err
	})
	return sp, err
}

// DeleteSpace physically removes the space and everything in it.
func (s *Store) DeleteSpace(ctx context.Context, projectID uuid.UUID, externalID string) error {
	return s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		n, err := q.DeleteSpace(ctx, externalID)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrSpaceNotFound
		}
		return nil
	})
}

// Retain stores the memories in one transaction. An item whose idempotency
// key already exists is a replay (Created=false) when its hash matches, and
// aborts the whole transaction with domain.ErrIdempotencyConflict otherwise.
func (s *Store) Retain(ctx context.Context, projectID uuid.UUID, externalID string, items []domain.NewMemory) ([]domain.RetainResult, error) {
	out := make([]domain.RetainResult, len(items))
	err := s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		for i, m := range items {
			params, err := insertParams(projectID, space.ID, m)
			if err != nil {
				return err
			}
			row, err := q.InsertMemory(ctx, params)
			if err == nil {
				out[i].Created = true
				out[i].Memory, err = toMemory(memoryRow(row), externalID)
				if err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) || m.IdempotencyKey == nil {
				return err
			}
			existing, err := q.GetMemoryByIdempotencyKey(ctx, sqlcdb.GetMemoryByIdempotencyKeyParams{
				SpaceID: space.ID, IdempotencyKey: m.IdempotencyKey,
			})
			if err != nil {
				return err
			}
			mem, err := toMemory(memoryRow(existing), externalID)
			if err != nil {
				return err
			}
			if err := m.CheckReplay(mem); err != nil {
				return &RetainConflictError{Index: i, Err: err}
			}
			out[i].Memory = mem
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RetainConflictError identifies the batch item that reused an idempotency
// key with different content.
type RetainConflictError struct {
	Index int
	Err   error
}

func (e *RetainConflictError) Error() string { return fmt.Sprintf("items[%d]: %v", e.Index, e.Err) }
func (e *RetainConflictError) Unwrap() error { return e.Err }

// GetMemory returns an active memory or domain.ErrMemoryNotFound.
func (s *Store) GetMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) (m domain.Memory, err error) {
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		row, err := q.GetActiveMemory(ctx, sqlcdb.GetActiveMemoryParams{SpaceID: space.ID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMemoryNotFound
		}
		if err != nil {
			return err
		}
		m, err = toMemory(memoryRow(row), externalID)
		return err
	})
	return m, err
}

// DeleteMemory soft-deletes a memory.
func (s *Store) DeleteMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) error {
	return s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		n, err := q.SoftDeleteMemory(ctx, sqlcdb.SoftDeleteMemoryParams{SpaceID: space.ID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrMemoryNotFound
		}
		return nil
	})
}

// ListMemories returns one page of active memories, newest first.
func (s *Store) ListMemories(ctx context.Context, projectID uuid.UUID, externalID string, lq domain.ListMemoriesQuery) (page domain.MemoryPage, err error) {
	params := sqlcdb.ListMemoriesParams{
		MemoryTypes:    memoryTypes(lq.MemoryTypes),
		OccurredAfter:  lq.OccurredAfter,
		OccurredBefore: lq.OccurredBefore,
		SourceType:     nonEmpty(lq.SourceType),
		SourceRef:      nonEmpty(lq.SourceRef),
		RowLimit:       int32(lq.Limit + 1), //nolint:gosec // limit is validated (<= 100)
	}
	if lq.After != nil {
		params.AfterOccurredAt, params.AfterID = &lq.After.OccurredAt, &lq.After.ID
	}
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		params.SpaceID = space.ID
		rows, err := q.ListMemories(ctx, params)
		if err != nil {
			return err
		}
		if len(rows) > lq.Limit {
			rows = rows[:lq.Limit]
			last := rows[len(rows)-1]
			page.Next = &domain.MemoryCursor{OccurredAt: last.OccurredAt, ID: last.ID}
		}
		page.Items = make([]domain.Memory, len(rows))
		for i, r := range rows {
			if page.Items[i], err = toMemory(memoryRow(r), externalID); err != nil {
				return err
			}
		}
		return nil
	})
	return page, err
}

// RecallCandidates runs the strategies requested by rq in one transaction
// and returns each ranked candidate list.
func (s *Store) RecallCandidates(ctx context.Context, projectID uuid.UUID, externalID string, rq domain.RecallQuery) (c domain.RecallCandidates, err error) {
	var md []byte
	if rq.Filters.Metadata != nil {
		if md, err = json.Marshal(rq.Filters.Metadata); err != nil {
			return c, err
		}
	}
	types := memoryTypes(rq.Filters.MemoryTypes)
	f := rq.Filters
	limit := int32(rq.CandidateLimit) //nolint:gosec // validated (<= 200)
	err = s.WithProject(ctx, projectID, func(q *sqlcdb.Queries) error {
		space, err := getSpace(ctx, q, externalID)
		if err != nil {
			return err
		}
		if rq.Lexical() {
			rows, err := q.RecallLexical(ctx, sqlcdb.RecallLexicalParams{
				Query: rq.Query, SpaceID: space.ID, MemoryTypes: types,
				OccurredAfter: f.OccurredAfter, OccurredBefore: f.OccurredBefore,
				SourceType: nonEmpty(f.SourceType), SourceRef: nonEmpty(f.SourceRef),
				Metadata: md, CandidateLimit: limit,
			})
			if err != nil {
				return err
			}
			c.Lexical = make([]domain.RecallItem, len(rows))
			for i, r := range rows {
				m, rank, err := rankedToMemory(rankedRow(r), externalID)
				if err != nil {
					return err
				}
				c.Lexical[i] = domain.RecallItem{Memory: m, LexicalRank: &rank}
			}
		}
		if rq.Semantic() {
			rows, err := q.RecallSemantic(ctx, sqlcdb.RecallSemanticParams{
				QueryVector: pgvector.NewVector(rq.QueryEmbedding.Vector), SpaceID: space.ID,
				EmbeddingModel: rq.QueryEmbedding.Model, MemoryTypes: types,
				OccurredAfter: f.OccurredAfter, OccurredBefore: f.OccurredBefore,
				SourceType: nonEmpty(f.SourceType), SourceRef: nonEmpty(f.SourceRef),
				Metadata: md, CandidateLimit: limit,
			})
			if err != nil {
				return err
			}
			c.Semantic = make([]domain.RecallItem, len(rows))
			for i, r := range rows {
				m, rank, err := rankedToMemory(rankedRow(r), externalID)
				if err != nil {
					return err
				}
				c.Semantic[i] = domain.RecallItem{Memory: m, SemanticRank: &rank}
			}
		}
		return nil
	})
	return c, err
}

// rankedRow is the column list of the recall queries: the memory columns
// plus the 1-based rank.
type rankedRow sqlcdb.RecallLexicalRow

func rankedToMemory(r rankedRow, externalID string) (domain.Memory, int, error) {
	m, err := toMemory(memoryRow{
		ID: r.ID, ProjectID: r.ProjectID, SpaceID: r.SpaceID, MemoryType: r.MemoryType,
		Content: r.Content, Summary: r.Summary, EmbeddingModel: r.EmbeddingModel,
		SourceType: r.SourceType, SourceRef: r.SourceRef, IdempotencyKey: r.IdempotencyKey,
		IdempotencyHash: r.IdempotencyHash, Importance: r.Importance, OccurredAt: r.OccurredAt,
		ExpiresAt: r.ExpiresAt, Metadata: r.Metadata, CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
	}, externalID)
	return m, int(r.Rank), err
}

func getSpace(ctx context.Context, q *sqlcdb.Queries, externalID string) (sqlcdb.MemorySpace, error) {
	row, err := q.GetSpaceByExternalID(ctx, externalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, domain.ErrSpaceNotFound
	}
	return row, err
}

// memoryRow is the column list shared by the memory queries; their
// generated row types convert to it.
type memoryRow sqlcdb.InsertMemoryRow

func toMemory(r memoryRow, externalID string) (domain.Memory, error) {
	var md domain.Metadata
	if err := json.Unmarshal(r.Metadata, &md); err != nil {
		return domain.Memory{}, fmt.Errorf("decode memory metadata: %w", err)
	}
	return domain.Memory{
		ID:              r.ID,
		ProjectID:       r.ProjectID,
		SpaceID:         r.SpaceID,
		SpaceExternalID: externalID,
		MemoryType:      domain.MemoryType(r.MemoryType),
		Content:         r.Content,
		Summary:         r.Summary,
		EmbeddingModel:  r.EmbeddingModel,
		Source:          domain.Source{Type: deref(r.SourceType), Ref: deref(r.SourceRef)},
		IdempotencyKey:  r.IdempotencyKey,
		IdempotencyHash: r.IdempotencyHash,
		Importance:      r.Importance,
		OccurredAt:      r.OccurredAt,
		ExpiresAt:       r.ExpiresAt,
		Metadata:        md,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		DeletedAt:       r.DeletedAt,
	}, nil
}

func toSpace(r sqlcdb.MemorySpace) (domain.Space, error) {
	var md domain.Metadata
	if err := json.Unmarshal(r.Metadata, &md); err != nil {
		return domain.Space{}, fmt.Errorf("decode space metadata: %w", err)
	}
	return domain.Space{
		ID: r.ID, ProjectID: r.ProjectID, ExternalID: r.ExternalID, Name: r.Name,
		Kind: domain.SpaceKind(r.Kind), Metadata: md, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func insertParams(projectID, spaceID uuid.UUID, m domain.NewMemory) (sqlcdb.InsertMemoryParams, error) {
	md, err := json.Marshal(m.Metadata)
	if err != nil {
		return sqlcdb.InsertMemoryParams{}, err
	}
	p := sqlcdb.InsertMemoryParams{
		ProjectID:       projectID,
		SpaceID:         spaceID,
		MemoryType:      string(m.MemoryType),
		Content:         m.Content,
		Summary:         m.Summary,
		SourceType:      nonEmpty(m.Source.Type),
		SourceRef:       nonEmpty(m.Source.Ref),
		IdempotencyKey:  m.IdempotencyKey,
		IdempotencyHash: m.IdempotencyHash,
		Importance:      m.Importance,
		OccurredAt:      m.OccurredAt,
		ExpiresAt:       m.ExpiresAt,
		Metadata:        md,
	}
	if m.Embedding != nil {
		v := pgvector.NewVector(m.Embedding.Vector)
		p.Embedding, p.EmbeddingModel = &v, &m.Embedding.Model
	}
	return p, nil
}

func memoryTypes(ts []domain.MemoryType) []string {
	if len(ts) == 0 {
		return nil
	}
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

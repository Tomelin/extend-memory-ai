// Package usecase holds the application use cases: they validate input with
// the domain entities, call the Store port and emit spans and metrics.
package usecase

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

const instrumentationName = "github.com/Tomelin/extend-memory-ai/internal/usecase"

// rrfK is the Reciprocal Rank Fusion constant: score = Σ 1 / (rrfK + rank).
const rrfK = 60

// Store is the persistence port. Every method runs inside the project's
// tenant context.
type Store interface {
	UpsertSpace(ctx context.Context, projectID uuid.UUID, in domain.SpaceInput) (domain.Space, bool, error)
	GetSpace(ctx context.Context, projectID uuid.UUID, externalID string) (domain.Space, error)
	DeleteSpace(ctx context.Context, projectID uuid.UUID, externalID string) error
	Retain(ctx context.Context, projectID uuid.UUID, externalID string, items []domain.NewMemory) ([]domain.RetainResult, error)
	GetMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) (domain.Memory, error)
	DeleteMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) error
	ListMemories(ctx context.Context, projectID uuid.UUID, externalID string, q domain.ListMemoriesQuery) (domain.MemoryPage, error)
	RecallLexical(ctx context.Context, projectID uuid.UUID, externalID string, q domain.RecallQuery) ([]domain.RecallItem, error)
}

// TokenCounter estimates how many tokens a text uses.
type TokenCounter func(text string) int

// EstimateTokens is the default TokenCounter: ceil(runes / 4).
func EstimateTokens(text string) int {
	return (utf8.RuneCountInString(text) + 3) / 4
}

// Options customizes New.
type Options struct {
	Now         func() time.Time
	CountTokens TokenCounter
}

// Service implements the space, memory and recall use cases.
type Service struct {
	store       Store
	now         func() time.Time
	countTokens TokenCounter
	tracer      trace.Tracer

	retainItems      metric.Int64Counter
	recallDuration   metric.Float64Histogram
	recallCandidates metric.Int64Histogram
	recallResults    metric.Int64Histogram
	recallTruncated  metric.Int64Counter
}

// New creates the Service.
func New(store Store, opts Options) (*Service, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.CountTokens == nil {
		opts.CountTokens = EstimateTokens
	}
	s := &Service{store: store, now: opts.Now, countTokens: opts.CountTokens, tracer: otel.Tracer(instrumentationName)}
	meter := otel.Meter(instrumentationName)
	var errs [5]error
	s.retainItems, errs[0] = meter.Int64Counter("memory.retain.items",
		metric.WithDescription("Memories processed by retain, by result."), metric.WithUnit("{memory}"))
	s.recallDuration, errs[1] = meter.Float64Histogram("memory.recall.duration",
		metric.WithDescription("Duration of the recall use case."), metric.WithUnit("s"))
	s.recallCandidates, errs[2] = meter.Int64Histogram("memory.recall.candidates",
		metric.WithDescription("Candidates returned by each retrieval strategy."), metric.WithUnit("{memory}"))
	s.recallResults, errs[3] = meter.Int64Histogram("memory.recall.results",
		metric.WithDescription("Memories returned by recall."), metric.WithUnit("{memory}"))
	s.recallTruncated, errs[4] = meter.Int64Counter("memory.recall.truncated",
		metric.WithDescription("Recalls whose results were cut by the token budget."), metric.WithUnit("{request}"))
	if err := errors.Join(errs[:]...); err != nil {
		return nil, err
	}
	return s, nil
}

// UpsertSpace creates or updates a space; created reports a new space.
func (s *Service) UpsertSpace(ctx context.Context, projectID uuid.UUID, in domain.SpaceInput) (sp domain.Space, created bool, err error) {
	ctx, span := s.tracer.Start(ctx, "app.UpsertSpace")
	defer func() { end(span, err) }()
	if in, err = in.Prepare(); err != nil {
		return domain.Space{}, false, err
	}
	return s.store.UpsertSpace(ctx, projectID, in)
}

// GetSpace returns a space.
func (s *Service) GetSpace(ctx context.Context, projectID uuid.UUID, externalID string) (sp domain.Space, err error) {
	ctx, span := s.tracer.Start(ctx, "app.GetSpace")
	defer func() { end(span, err) }()
	return s.store.GetSpace(ctx, projectID, externalID)
}

// DeleteSpace physically removes a space and its memories.
func (s *Service) DeleteSpace(ctx context.Context, projectID uuid.UUID, externalID string) (err error) {
	ctx, span := s.tracer.Start(ctx, "app.DeleteSpace")
	defer func() { end(span, err) }()
	return s.store.DeleteSpace(ctx, projectID, externalID)
}

// Retain stores one memory.
func (s *Service) Retain(ctx context.Context, projectID uuid.UUID, externalID string, in domain.MemoryInput) (res domain.RetainResult, err error) {
	ctx, span := s.tracer.Start(ctx, "app.Retain")
	defer func() { end(span, err) }()
	m, err := in.Prepare(s.now())
	if err != nil {
		return domain.RetainResult{}, err
	}
	results, err := s.store.Retain(ctx, projectID, externalID, []domain.NewMemory{m})
	s.recordRetain(ctx, results, err)
	if err != nil {
		return domain.RetainResult{}, err
	}
	return results[0], nil
}

// RetainBatch stores all memories or none.
func (s *Service) RetainBatch(ctx context.Context, projectID uuid.UUID, externalID string, in []domain.MemoryInput) (res []domain.RetainResult, err error) {
	ctx, span := s.tracer.Start(ctx, "app.RetainBatch", trace.WithAttributes(attribute.Int("batch.size", len(in))))
	defer func() { end(span, err) }()
	items, err := domain.PrepareBatch(in, s.now())
	if err != nil {
		return nil, err
	}
	res, err = s.store.Retain(ctx, projectID, externalID, items)
	s.recordRetain(ctx, res, err)
	return res, err
}

func (s *Service) recordRetain(ctx context.Context, results []domain.RetainResult, err error) {
	if errors.Is(err, domain.ErrIdempotencyConflict) {
		s.retainItems.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "conflict")))
		return
	}
	for _, r := range results {
		result := "replayed"
		if r.Created {
			result = "created"
		}
		s.retainItems.Add(ctx, 1, metric.WithAttributes(
			attribute.String("result", result),
			attribute.String("memory_type", string(r.Memory.MemoryType)),
		))
	}
}

// GetMemory returns an active memory.
func (s *Service) GetMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) (m domain.Memory, err error) {
	ctx, span := s.tracer.Start(ctx, "app.GetMemory")
	defer func() { end(span, err) }()
	return s.store.GetMemory(ctx, projectID, externalID, id)
}

// DeleteMemory soft-deletes a memory.
func (s *Service) DeleteMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) (err error) {
	ctx, span := s.tracer.Start(ctx, "app.DeleteMemory")
	defer func() { end(span, err) }()
	return s.store.DeleteMemory(ctx, projectID, externalID, id)
}

// ListMemories returns one page of active memories, newest first.
func (s *Service) ListMemories(ctx context.Context, projectID uuid.UUID, externalID string, in domain.ListMemoriesInput) (page domain.MemoryPage, err error) {
	ctx, span := s.tracer.Start(ctx, "app.ListMemories")
	defer func() { end(span, err) }()
	q, err := in.Prepare()
	if err != nil {
		return domain.MemoryPage{}, err
	}
	return s.store.ListMemories(ctx, projectID, externalID, q)
}

// Recall runs the full-text strategy, ranks the candidates and applies the
// limit and the token budget. The semantic strategy is not available yet:
// query_embedding alone is rejected and, next to query, ignored.
func (s *Service) Recall(ctx context.Context, projectID uuid.UUID, externalID string, in domain.RecallInput) (res domain.RecallResult, err error) {
	ctx, span := s.tracer.Start(ctx, "app.Recall")
	defer func() { end(span, err) }()
	start := time.Now()

	q, err := in.Prepare()
	if err != nil {
		return domain.RecallResult{}, err
	}
	if !q.Lexical() {
		return domain.RecallResult{}, &domain.ValidationError{Fields: []domain.FieldError{{
			Field: "query", Message: "a busca semântica ainda não está disponível; informe query",
		}}}
	}

	candidates, err := s.store.RecallLexical(ctx, projectID, externalID, q)
	if err != nil {
		return domain.RecallResult{}, err
	}
	strategy := metric.WithAttributes(attribute.String("strategy", string(domain.MatchLexical)))
	s.recallCandidates.Record(ctx, int64(len(candidates)), strategy)
	span.AddEvent("lexical_candidates", trace.WithAttributes(attribute.Int("count", len(candidates))))

	res = s.rank(candidates, q)

	span.AddEvent("returned", trace.WithAttributes(attribute.Int("count", len(res.Items))))
	s.recallResults.Record(ctx, int64(len(res.Items)))
	if res.Truncated {
		span.AddEvent("truncated", trace.WithAttributes(attribute.Int("token_estimate", res.TokenEstimate)))
		s.recallTruncated.Add(ctx, 1)
	}
	s.recallDuration.Record(ctx, time.Since(start).Seconds(), strategy)
	return res, nil
}

// rank scores candidates with RRF and stops at the limit or before the
// token budget is exceeded.
func (s *Service) rank(candidates []domain.RecallItem, q domain.RecallQuery) domain.RecallResult {
	res := domain.RecallResult{Items: make([]domain.RecallItem, 0, min(len(candidates), q.Limit))}
	for _, c := range candidates {
		if len(res.Items) == q.Limit {
			break
		}
		tokens := s.countTokens(c.Memory.Content)
		if q.MaxTokens != nil && res.TokenEstimate+tokens > *q.MaxTokens {
			res.Truncated = true
			break
		}
		c.Score = 0
		for _, r := range []*int{c.LexicalRank, c.SemanticRank} {
			if r != nil {
				c.Score += 1.0 / float64(rrfK+*r)
			}
		}
		res.TokenEstimate += tokens
		res.Items = append(res.Items, c)
	}
	return res
}

// end closes the span; only unexpected errors mark it as failed, so 4xx
// outcomes do not inflate the trace error rate.
func end(span trace.Span, err error) {
	if err != nil && !isClientError(err) {
		span.RecordError(err)
		span.SetStatus(codes.Error, "use case failed")
	}
	span.End()
}

func isClientError(err error) bool {
	return errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, domain.ErrConflict) ||
		errors.Is(err, domain.ErrValidation)
}

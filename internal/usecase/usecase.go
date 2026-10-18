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
	RecallCandidates(ctx context.Context, projectID uuid.UUID, externalID string, q domain.RecallQuery) (domain.RecallCandidates, error)
	LinksOf(ctx context.Context, projectID uuid.UUID, externalID string, memoryIDs []uuid.UUID) ([]domain.Link, error)
	CreateLink(ctx context.Context, projectID uuid.UUID, externalID string, l domain.NewLink) (domain.Link, error)
	ListLinks(ctx context.Context, projectID uuid.UUID, externalID string, memoryID uuid.UUID, f domain.LinkFilter) ([]domain.Link, error)
	DeleteLink(ctx context.Context, projectID uuid.UUID, externalID string, source, target uuid.UUID, relation domain.RelationType) error
	ListSpaces(ctx context.Context, projectID uuid.UUID, q domain.ListSpacesQuery) (domain.SpacePage, error)
	UpdateMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID, apply func(domain.Memory) (domain.MemoryUpdate, error)) (domain.Memory, error)
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

// ListSpaces returns one page of the project's spaces.
func (s *Service) ListSpaces(ctx context.Context, projectID uuid.UUID, in domain.ListSpacesInput) (page domain.SpacePage, err error) {
	ctx, span := s.tracer.Start(ctx, "app.ListSpaces")
	defer func() { end(span, err) }()
	q, err := in.Prepare()
	if err != nil {
		return domain.SpacePage{}, err
	}
	return s.store.ListSpaces(ctx, projectID, q)
}

// UpdateMemory applies a merge patch to an active memory.
func (s *Service) UpdateMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID, p domain.MemoryPatch) (m domain.Memory, err error) {
	ctx, span := s.tracer.Start(ctx, "app.UpdateMemory")
	defer func() { end(span, err) }()
	return s.store.UpdateMemory(ctx, projectID, externalID, id, p.Apply)
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

// Recall runs the requested strategies, fuses them with RRF, applies the
// limit and the token budget and, on request, attaches direct relations.
func (s *Service) Recall(ctx context.Context, projectID uuid.UUID, externalID string, in domain.RecallInput) (res domain.RecallResult, err error) {
	ctx, span := s.tracer.Start(ctx, "app.Recall")
	defer func() { end(span, err) }()
	start := time.Now()

	q, err := in.Prepare()
	if err != nil {
		return domain.RecallResult{}, err
	}
	c, err := s.store.RecallCandidates(ctx, projectID, externalID, q)
	if err != nil {
		return domain.RecallResult{}, err
	}
	if q.Lexical() {
		s.recordCandidates(ctx, span, domain.MatchLexical, len(c.Lexical))
	}
	if q.Semantic() {
		s.recordCandidates(ctx, span, domain.MatchSemantic, len(c.Semantic))
	}

	res = s.budget(domain.FuseRRF(c), q)
	if q.IncludeLinks {
		if err := s.attachLinks(ctx, projectID, externalID, res.Items); err != nil {
			return domain.RecallResult{}, err
		}
	}

	span.AddEvent("returned", trace.WithAttributes(attribute.Int("count", len(res.Items))))
	s.recallResults.Record(ctx, int64(len(res.Items)))
	if res.Truncated {
		span.AddEvent("truncated", trace.WithAttributes(attribute.Int("token_estimate", res.TokenEstimate)))
		s.recallTruncated.Add(ctx, 1)
	}
	s.recallDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(attribute.String("strategy", strategy(q))))
	return res, nil
}

func (s *Service) recordCandidates(ctx context.Context, span trace.Span, st domain.MatchStrategy, n int) {
	s.recallCandidates.Record(ctx, int64(n), metric.WithAttributes(attribute.String("strategy", string(st))))
	span.AddEvent(string(st)+"_candidates", trace.WithAttributes(attribute.Int("count", n)))
}

func strategy(q domain.RecallQuery) string {
	switch {
	case q.Lexical() && q.Semantic():
		return "hybrid"
	case q.Semantic():
		return string(domain.MatchSemantic)
	}
	return string(domain.MatchLexical)
}

// budget keeps the fused order and stops at the limit or before the token
// budget is exceeded.
func (s *Service) budget(fused []domain.RecallItem, q domain.RecallQuery) domain.RecallResult {
	res := domain.RecallResult{Items: make([]domain.RecallItem, 0, min(len(fused), q.Limit))}
	for _, it := range fused {
		if len(res.Items) == q.Limit {
			break
		}
		tokens := s.countTokens(it.Memory.Content)
		if q.MaxTokens != nil && res.TokenEstimate+tokens > *q.MaxTokens {
			res.Truncated = true
			break
		}
		res.TokenEstimate += tokens
		res.Items = append(res.Items, it)
	}
	return res
}

// attachLinks adds the direct relations (both directions) of each item,
// without following them recursively.
func (s *Service) attachLinks(ctx context.Context, projectID uuid.UUID, externalID string, items []domain.RecallItem) error {
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.Memory.ID
		items[i].Links = []domain.Link{}
	}
	if len(ids) == 0 {
		return nil
	}
	links, err := s.store.LinksOf(ctx, projectID, externalID, ids)
	if err != nil {
		return err
	}
	for i := range items {
		id := items[i].Memory.ID
		for _, l := range links {
			if l.SourceMemoryID == id || l.TargetMemoryID == id {
				items[i].Links = append(items[i].Links, l)
			}
		}
	}
	return nil
}

// CreateLink relates two memories of the same space.
func (s *Service) CreateLink(ctx context.Context, projectID uuid.UUID, externalID string, in domain.LinkInput) (l domain.Link, err error) {
	ctx, span := s.tracer.Start(ctx, "app.CreateLink")
	defer func() { end(span, err) }()
	nl, err := in.Prepare()
	if err != nil {
		return domain.Link{}, err
	}
	return s.store.CreateLink(ctx, projectID, externalID, nl)
}

// ListLinks returns the relations of a memory.
func (s *Service) ListLinks(ctx context.Context, projectID uuid.UUID, externalID string, memoryID uuid.UUID, f domain.LinkFilter) (links []domain.Link, err error) {
	ctx, span := s.tracer.Start(ctx, "app.ListLinks")
	defer func() { end(span, err) }()
	if f, err = f.Prepare(); err != nil {
		return nil, err
	}
	return s.store.ListLinks(ctx, projectID, externalID, memoryID, f)
}

// DeleteLink removes a relation.
func (s *Service) DeleteLink(ctx context.Context, projectID uuid.UUID, externalID string, source, target uuid.UUID, relation domain.RelationType) (err error) {
	ctx, span := s.tracer.Start(ctx, "app.DeleteLink")
	defer func() { end(span, err) }()
	if !relation.Valid() {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "relation_type", Message: "valor não aceito"}}}
	}
	return s.store.DeleteLink(ctx, projectID, externalID, source, target, relation)
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

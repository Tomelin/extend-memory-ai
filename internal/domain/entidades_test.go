package domain_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

// TestEntidadesFeature runs features/entidades.feature against the domain
// package only; no database or HTTP server is involved.
func TestEntidadesFeature(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeEntidades,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/entidades.feature"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("features/entidades.feature failed")
	}
}

var retainInstant = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

type subject int

const (
	subjectSpace subject = iota + 1
	subjectMemory
	subjectRecall
)

type entidades struct {
	subject subject
	space   domain.SpaceInput
	memory  domain.MemoryInput
	batch   []domain.MemoryInput
	recall  domain.RecallInput

	err           error
	preparedSpace domain.SpaceInput
	prepared      domain.NewMemory
	query         domain.RecallQuery

	stored    domain.Memory
	replayErr error
}

func initializeEntidades(sc *godog.ScenarioContext) {
	s := &entidades{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*s = entidades{}
		return ctx, nil
	})

	sc.Step(`^um espaço "([^"]*)" com nome "([^"]*)"$`, s.aSpace)
	sc.Step(`^preparo o espaço$`, s.prepareSpace)
	sc.Step(`^o tipo do espaço deve ser "([^"]*)"$`, s.spaceKindShouldBe)

	sc.Step(`^uma memória com conteúdo "([^"]*)"$`, s.aMemory)
	sc.Step(`^com "([^"]*)" igual a "([^"]*)"$`, s.withField)
	sc.Step(`^com um embedding de (\d+) dimensões do modelo "([^"]*)"$`, s.withEmbedding)
	sc.Step(`^preparo a memória para gravação$`, s.prepareMemory)
	sc.Step(`^o tipo da memória deve ser "([^"]*)"$`, s.memoryTypeShouldBe)
	sc.Step(`^a importância deve ser ([\d.]+)$`, s.importanceShouldBe)
	sc.Step(`^o momento da ocorrência deve ser o instante da gravação$`, s.occurredAtShouldBeRetainInstant)
	sc.Step(`^o momento da ocorrência deve ser "([^"]*)"$`, s.occurredAtShouldBe)
	sc.Step(`^a origem deve ser "([^"]*)" com referência "([^"]*)"$`, s.sourceShouldBe)

	sc.Step(`^que a memória foi preparada e gravada$`, s.preparedAndStored)
	sc.Step(`^preparo a mesma memória novamente uma hora depois$`, s.replayLater)
	sc.Step(`^preparo a mesma memória novamente com um embedding de (\d+) dimensões do modelo "([^"]*)"$`, s.replayWithEmbedding)
	sc.Step(`^preparo a mesma memória novamente com "([^"]*)" igual a "([^"]*)"$`, s.replayWithField)
	sc.Step(`^a repetição deve ser aceita como idempotente$`, s.replayAccepted)
	sc.Step(`^a repetição deve ser rejeitada por conflito de idempotência$`, s.replayConflict)

	sc.Step(`^um lote com (\d+) memórias válidas$`, s.aBatch)
	sc.Step(`^o item (\d+) do lote tem "([^"]*)" igual a "([^"]*)"$`, s.batchItemField)
	sc.Step(`^preparo o lote$`, s.prepareBatch)

	sc.Step(`^uma consulta de recall com o texto "([^"]*)"$`, s.aRecallWithText)
	sc.Step(`^uma consulta de recall sem texto$`, s.aRecallWithoutText)
	sc.Step(`^preparo a consulta$`, s.prepareRecall)
	sc.Step(`^o limite deve ser (\d+) e o limite de candidatos (\d+)$`, s.limitsShouldBe)
	sc.Step(`^a consulta deve usar somente a busca "([^"]*)"$`, s.onlyStrategy)

	sc.Step(`^a preparação deve ser aceita$`, s.accepted)
	sc.Step(`^a preparação deve falhar no campo "([^"]*)"$`, s.failedOnField)
}

func (s *entidades) aSpace(externalID, name string) {
	s.subject = subjectSpace
	s.space = domain.SpaceInput{ExternalID: externalID, Name: name}
}

func (s *entidades) prepareSpace() {
	s.preparedSpace, s.err = s.space.Prepare()
}

func (s *entidades) spaceKindShouldBe(kind string) error {
	return expectEqual("kind", domain.SpaceKind(kind), s.preparedSpace.Kind)
}

func (s *entidades) aMemory(content string) {
	s.subject = subjectMemory
	s.memory = domain.MemoryInput{Content: content}
}

func (s *entidades) withField(field, value string) error {
	switch s.subject {
	case subjectSpace:
		return setSpaceField(&s.space, field, value)
	case subjectMemory:
		return setMemoryField(&s.memory, field, value)
	case subjectRecall:
		return setRecallField(&s.recall, field, value)
	}
	return errors.New("no subject defined")
}

func (s *entidades) withEmbedding(dims int, model string) error {
	e := embedding(dims, model)
	switch s.subject {
	case subjectMemory:
		s.memory.Embedding = e
	case subjectRecall:
		s.recall.QueryEmbedding = e
	default:
		return errors.New("embedding requires a memory or recall subject")
	}
	return nil
}

func (s *entidades) prepareMemory() {
	s.prepared, s.err = s.memory.Prepare(retainInstant)
}

func (s *entidades) memoryTypeShouldBe(t string) error {
	return expectEqual("memory_type", domain.MemoryType(t), s.prepared.MemoryType)
}

func (s *entidades) importanceShouldBe(raw string) error {
	want, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		return err
	}
	return expectEqual("importance", float32(want), s.prepared.Importance)
}

func (s *entidades) occurredAtShouldBeRetainInstant() error {
	return expectEqual("occurred_at", retainInstant, s.prepared.OccurredAt)
}

func (s *entidades) occurredAtShouldBe(raw string) error {
	want, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return err
	}
	if !s.prepared.OccurredAt.Equal(want) || s.prepared.OccurredAt.Location() != time.UTC {
		return fmt.Errorf("occurred_at: want %s in UTC, got %s", want, s.prepared.OccurredAt)
	}
	return nil
}

func (s *entidades) sourceShouldBe(typ, ref string) error {
	return expectEqual("source", domain.Source{Type: typ, Ref: ref}, s.prepared.Source)
}

func (s *entidades) preparedAndStored() error {
	m, err := s.memory.Prepare(retainInstant)
	if err != nil {
		return err
	}
	if len(m.IdempotencyHash) == 0 {
		return errors.New("idempotency hash was not computed")
	}
	s.stored = domain.Memory{IdempotencyKey: m.IdempotencyKey, IdempotencyHash: m.IdempotencyHash}
	return nil
}

func (s *entidades) replay(in domain.MemoryInput, now time.Time) error {
	m, err := in.Prepare(now)
	if err != nil {
		return err
	}
	s.replayErr = m.CheckReplay(s.stored)
	return nil
}

func (s *entidades) replayLater() error {
	return s.replay(s.memory, retainInstant.Add(time.Hour))
}

func (s *entidades) replayWithEmbedding(dims int, model string) error {
	in := s.memory
	in.Embedding = embedding(dims, model)
	return s.replay(in, retainInstant)
}

func (s *entidades) replayWithField(field, value string) error {
	in := s.memory
	if err := setMemoryField(&in, field, value); err != nil {
		return err
	}
	return s.replay(in, retainInstant)
}

func (s *entidades) replayAccepted() error {
	if s.replayErr != nil {
		return fmt.Errorf("expected idempotent replay, got %w", s.replayErr)
	}
	return nil
}

func (s *entidades) replayConflict() error {
	if !errors.Is(s.replayErr, domain.ErrIdempotencyConflict) || !errors.Is(s.replayErr, domain.ErrConflict) {
		return fmt.Errorf("expected idempotency conflict, got %w", s.replayErr)
	}
	return nil
}

func (s *entidades) aBatch(n int) {
	s.subject = subjectMemory
	s.batch = make([]domain.MemoryInput, n)
	for i := range s.batch {
		s.batch[i] = domain.MemoryInput{Content: fmt.Sprintf("memória %d", i+1)}
	}
}

func (s *entidades) batchItemField(position int, field, value string) error {
	if position < 1 || position > len(s.batch) {
		return fmt.Errorf("batch has no item %d", position)
	}
	return setMemoryField(&s.batch[position-1], field, value)
}

func (s *entidades) prepareBatch() {
	_, s.err = domain.PrepareBatch(s.batch, retainInstant)
}

func (s *entidades) aRecallWithText(text string) {
	s.subject = subjectRecall
	s.recall = domain.RecallInput{Query: &text}
}

func (s *entidades) aRecallWithoutText() {
	s.subject = subjectRecall
	s.recall = domain.RecallInput{}
}

func (s *entidades) prepareRecall() {
	s.query, s.err = s.recall.Prepare()
}

func (s *entidades) limitsShouldBe(limit, candidates int) error {
	if err := expectEqual("limit", limit, s.query.Limit); err != nil {
		return err
	}
	return expectEqual("candidate_limit", candidates, s.query.CandidateLimit)
}

func (s *entidades) onlyStrategy(strategy string) error {
	got := map[domain.MatchStrategy]bool{
		domain.MatchLexical:  s.query.Lexical(),
		domain.MatchSemantic: s.query.Semantic(),
	}
	for st, on := range got {
		if on != (st == domain.MatchStrategy(strategy)) {
			return fmt.Errorf("strategy %s: enabled=%v", st, on)
		}
	}
	return nil
}

func (s *entidades) accepted() error {
	if s.err != nil {
		return fmt.Errorf("expected success, got %w", s.err)
	}
	return nil
}

func (s *entidades) failedOnField(field string) error {
	if !errors.Is(s.err, domain.ErrValidation) {
		return fmt.Errorf("expected validation error, got %w", s.err)
	}
	var ve *domain.ValidationError
	if !errors.As(s.err, &ve) {
		return fmt.Errorf("expected *domain.ValidationError, got %T", s.err)
	}
	if !slices.ContainsFunc(ve.Fields, func(f domain.FieldError) bool { return f.Field == field }) {
		return fmt.Errorf("expected error on field %q, got %v", field, ve.Fields)
	}
	return nil
}

func setSpaceField(in *domain.SpaceInput, field, value string) error {
	switch field {
	case "kind":
		in.Kind = domain.SpaceKind(value)
	default:
		return fmt.Errorf("unknown space field %q", field)
	}
	return nil
}

func setMemoryField(in *domain.MemoryInput, field, value string) error {
	switch field {
	case "content":
		in.Content = value
	case "memory_type":
		in.MemoryType = domain.MemoryType(value)
	case "summary":
		in.Summary = &value
	case "source.type":
		in.Source.Type = value
	case "source.ref":
		in.Source.Ref = value
	case "idempotency_key":
		in.IdempotencyKey = &value
	case "importance":
		f, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return err
		}
		v := float32(f)
		in.Importance = &v
	case "occurred_at":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return err
		}
		in.OccurredAt = &t
	case "expires_at":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return err
		}
		in.ExpiresAt = &t
	default:
		return fmt.Errorf("unknown memory field %q", field)
	}
	return nil
}

func setRecallField(in *domain.RecallInput, field, value string) error {
	switch field {
	case "limit", "candidate_limit", "max_tokens":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		switch field {
		case "limit":
			in.Limit = &n
		case "candidate_limit":
			in.CandidateLimit = &n
		default:
			in.MaxTokens = &n
		}
	case "filters.memory_types":
		in.Filters.MemoryTypes = append(in.Filters.MemoryTypes, domain.MemoryType(value))
	case "filters.occurred_after", "filters.occurred_before":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return err
		}
		if field == "filters.occurred_after" {
			in.Filters.OccurredAfter = &t
		} else {
			in.Filters.OccurredBefore = &t
		}
	default:
		return fmt.Errorf("unknown recall field %q", field)
	}
	return nil
}

func embedding(dims int, model string) *domain.Embedding {
	v := make([]float32, dims)
	for i := range v {
		v[i] = float32(i%7) / 10
	}
	return &domain.Embedding{Model: model, Vector: v}
}

func expectEqual[T comparable](field string, want, got T) error {
	if want != got {
		return fmt.Errorf("%s: want %v, got %v", field, want, got)
	}
	return nil
}

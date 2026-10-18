package domain_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"

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
	subjectLink
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

	link     domain.LinkInput
	newLink  domain.NewLink
	ids      map[string]uuid.UUID
	lexical  []domain.RecallItem
	semantic []domain.RecallItem
	fused    []domain.RecallItem

	current domain.Memory
	update  domain.MemoryUpdate
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

	sc.Step(`^que a busca (textual|semântica) encontrou "([^"]*)"$`, s.strategyFound)
	sc.Step(`^combino os resultados$`, s.fuse)
	sc.Step(`^a ordem deve ser "([^"]*)"$`, s.orderShouldBe)
	sc.Step(`^"([^"]*)" deve ter sido encontrada por "([^"]*)"$`, s.matchedBy)

	sc.Step(`^uma relação "([^"]*)" para outra memória$`, s.aLink)
	sc.Step(`^uma relação "([^"]*)" da memória para ela mesma$`, s.aSelfLink)
	sc.Step(`^preparo a relação$`, s.prepareLink)
	sc.Step(`^o peso da relação deve ser (\d+)$`, s.linkWeightShouldBe)

	sc.Step(`^uma memória gravada com embedding do modelo "([^"]*)"$`, s.storedWithEmbedding)
	sc.Step(`^uma memória gravada com metadata (\{.*\})$`, s.storedWithMetadata)
	sc.Step(`^aplico a alteração com "([^"]*)" igual a "([^"]*)"$`, s.applyField)
	sc.Step(`^aplico a alteração de metadata (\{.*\})$`, s.applyMetadata)
	sc.Step(`^o embedding deve ser "(removido|mantido)"$`, s.embeddingShouldBe)
	sc.Step(`^a metadata deve ser (\{.*\})$`, s.metadataShouldBe)
	sc.Step(`^uma listagem de espaços com "([^"]*)" igual a "([^"]*)"$`, s.spaceListing)
	sc.Step(`^preparo a listagem de espaços$`, func() {})

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
	case subjectLink:
		if field != "weight" {
			return fmt.Errorf("unknown link field %q", field)
		}
		f, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return err
		}
		w := float32(f)
		s.link.Weight = &w
		return nil
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

func (s *entidades) id(label string) uuid.UUID {
	if s.ids == nil {
		s.ids = map[string]uuid.UUID{}
	}
	if _, ok := s.ids[label]; !ok {
		s.ids[label] = uuid.New()
	}
	return s.ids[label]
}

func (s *entidades) strategyFound(strategy, labels string) {
	var items []domain.RecallItem
	for i, label := range strings.Split(labels, ", ") {
		rank := i + 1
		it := domain.RecallItem{Memory: domain.Memory{ID: s.id(label)}}
		if strategy == "textual" {
			it.LexicalRank = &rank
		} else {
			it.SemanticRank = &rank
		}
		items = append(items, it)
	}
	if strategy == "textual" {
		s.lexical = items
	} else {
		s.semantic = items
	}
}

func (s *entidades) fuse() {
	s.fused = domain.FuseRRF(domain.RecallCandidates{Lexical: s.lexical, Semantic: s.semantic})
}

func (s *entidades) orderShouldBe(labels string) error {
	want := strings.Split(labels, ", ")
	if len(want) != len(s.fused) {
		return fmt.Errorf("got %d items, want %d", len(s.fused), len(want))
	}
	for i, label := range want {
		if s.fused[i].Memory.ID != s.id(label) {
			return fmt.Errorf("position %d is not %s", i+1, label)
		}
	}
	return nil
}

func (s *entidades) matchedBy(label, strategies string) error {
	for _, it := range s.fused {
		if it.Memory.ID != s.id(label) {
			continue
		}
		var got []string
		for _, m := range it.MatchedBy() {
			got = append(got, string(m))
		}
		return expectEqual("matched_by", strategies, strings.Join(got, ", "))
	}
	return fmt.Errorf("%s not found", label)
}

func (s *entidades) aLink(relation string) {
	s.subject = subjectLink
	s.link = domain.LinkInput{SourceMemoryID: uuid.New(), TargetMemoryID: uuid.New(), RelationType: domain.RelationType(relation)}
}

func (s *entidades) aSelfLink(relation string) {
	s.aLink(relation)
	s.link.TargetMemoryID = s.link.SourceMemoryID
}

func (s *entidades) prepareLink() {
	s.newLink, s.err = s.link.Prepare()
}

func (s *entidades) linkWeightShouldBe(w int) error {
	return expectEqual("weight", float32(w), s.newLink.Weight)
}

func (s *entidades) storedWithEmbedding(model string) {
	s.current = domain.Memory{ID: uuid.New(), Content: "O usuário prefere relatórios em PDF",
		EmbeddingModel: &model, Importance: 0.5, OccurredAt: retainInstant, Metadata: domain.Metadata{}}
}

func (s *entidades) storedWithMetadata(raw string) error {
	s.storedWithEmbedding("modelo-a")
	return json.Unmarshal([]byte(raw), &s.current.Metadata)
}

func (s *entidades) applyField(field, value string) error {
	var p domain.MemoryPatch
	switch field {
	case "content":
		p.Content = domain.PatchField[string]{Set: true, Value: value}
	case "importance":
		f, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return err
		}
		p.Importance = domain.PatchField[float32]{Set: true, Value: float32(f)}
	case "expires_at":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return err
		}
		p.ExpiresAt = domain.PatchField[time.Time]{Set: true, Value: t}
	default:
		if !slices.Contains(domain.ImmutableMemoryFields, field) {
			return fmt.Errorf("unknown patch field %q", field)
		}
		p.Immutable = []string{field}
	}
	s.update, s.err = p.Apply(s.current)
	return nil
}

func (s *entidades) applyMetadata(raw string) error {
	var md domain.Metadata
	if err := json.Unmarshal([]byte(raw), &md); err != nil {
		return err
	}
	s.update, s.err = domain.MemoryPatch{Metadata: domain.PatchField[domain.Metadata]{Set: true, Value: md}}.Apply(s.current)
	return nil
}

func (s *entidades) embeddingShouldBe(state string) error {
	want := map[string]domain.EmbeddingChange{"removido": domain.EmbeddingClear, "mantido": domain.EmbeddingKeep}[state]
	return expectEqual("embedding", want, s.update.Embedding)
}

func (s *entidades) metadataShouldBe(raw string) error {
	var want domain.Metadata
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		return err
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(s.update.Metadata)
	return expectEqual("metadata", string(a), string(b))
}

func (s *entidades) spaceListing(field, value string) error {
	in := domain.ListSpacesInput{}
	switch field {
	case "kind":
		in.Kind = domain.SpaceKind(value)
	case "limit":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		in.Limit = &n
	default:
		return fmt.Errorf("unknown listing field %q", field)
	}
	_, s.err = in.Prepare()
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

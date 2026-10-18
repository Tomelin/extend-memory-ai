package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/usecase"
)

type fakeStore struct {
	usecase.Store
	candidates domain.RecallCandidates
	links      []domain.Link
	calls      int
}

func (f *fakeStore) RecallCandidates(context.Context, uuid.UUID, string, domain.RecallQuery) (domain.RecallCandidates, error) {
	f.calls++
	return f.candidates, nil
}

func (f *fakeStore) LinksOf(context.Context, uuid.UUID, string, []uuid.UUID) ([]domain.Link, error) {
	return f.links, nil
}

func (f *fakeStore) Retain(context.Context, uuid.UUID, string, []domain.NewMemory) ([]domain.RetainResult, error) {
	f.calls++
	return nil, nil
}

func candidates(n int, content string) domain.RecallCandidates {
	out := make([]domain.RecallItem, n)
	for i := range out {
		rank := i + 1
		out[i] = domain.RecallItem{Memory: domain.Memory{ID: uuid.New(), Content: content}, LexicalRank: &rank}
	}
	return domain.RecallCandidates{Lexical: out}
}

func newService(t *testing.T, store usecase.Store) *usecase.Service {
	t.Helper()
	s, err := usecase.New(store, usecase.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func TestRecallScoresWithRRFAndRespectsLimit(t *testing.T) {
	store := &fakeStore{candidates: candidates(5, "relatório")}
	res, err := newService(t, store).Recall(context.Background(), uuid.New(), "u", domain.RecallInput{Query: ptr("relatório"), Limit: ptr(3)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 || res.Truncated {
		t.Fatalf("items = %d, truncated = %v", len(res.Items), res.Truncated)
	}
	if want := 1.0 / 61; res.Items[0].Score != want {
		t.Fatalf("score = %v, want %v", res.Items[0].Score, want)
	}
	if res.TokenEstimate != 3*usecase.EstimateTokens("relatório") {
		t.Fatalf("token_estimate = %d", res.TokenEstimate)
	}
}

func TestRecallStopsBeforeExceedingTokenBudget(t *testing.T) {
	store := &fakeStore{candidates: candidates(20, strings.Repeat("a", 400))}
	res, err := newService(t, store).Recall(context.Background(), uuid.New(), "u", domain.RecallInput{Query: ptr("a"), MaxTokens: ptr(350)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 || !res.Truncated || res.TokenEstimate != 300 {
		t.Fatalf("items = %d, truncated = %v, tokens = %d", len(res.Items), res.Truncated, res.TokenEstimate)
	}
}

func TestRecallSemanticOnly(t *testing.T) {
	rank := 1
	m := domain.Memory{ID: uuid.New(), Content: "x"}
	store := &fakeStore{candidates: domain.RecallCandidates{Semantic: []domain.RecallItem{{Memory: m, SemanticRank: &rank}}}}
	e := &domain.Embedding{Model: "m", Vector: make([]float32, domain.EmbeddingDimensions)}
	res, err := newService(t, store).Recall(context.Background(), uuid.New(), "u", domain.RecallInput{QueryEmbedding: e})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].MatchedBy()[0] != domain.MatchSemantic {
		t.Fatalf("items = %+v", res.Items)
	}
}

func TestRecallAttachesDirectLinks(t *testing.T) {
	store := &fakeStore{candidates: candidates(2, "x")}
	a, b := store.candidates.Lexical[0].Memory.ID, store.candidates.Lexical[1].Memory.ID
	store.links = []domain.Link{{SourceMemoryID: a, TargetMemoryID: uuid.New(), RelationType: domain.RelationSupports}}
	res, err := newService(t, store).Recall(context.Background(), uuid.New(), "u", domain.RecallInput{Query: ptr("x"), IncludeLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range res.Items {
		want := map[uuid.UUID]int{a: 1, b: 0}[it.Memory.ID]
		if it.Links == nil || len(it.Links) != want {
			t.Fatalf("memory %s links = %v, want %d", it.Memory.ID, it.Links, want)
		}
	}
}

func TestRetainValidatesBeforeStore(t *testing.T) {
	store := &fakeStore{}
	_, err := newService(t, store).Retain(context.Background(), uuid.New(), "u", domain.MemoryInput{})
	if !errors.Is(err, domain.ErrValidation) || store.calls != 0 {
		t.Fatalf("err = %v, store calls = %d", err, store.calls)
	}
}

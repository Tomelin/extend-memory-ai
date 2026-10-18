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
	candidates []domain.RecallItem
	calls      int
}

func (f *fakeStore) RecallLexical(context.Context, uuid.UUID, string, domain.RecallQuery) ([]domain.RecallItem, error) {
	f.calls++
	return f.candidates, nil
}

func (f *fakeStore) Retain(context.Context, uuid.UUID, string, []domain.NewMemory) ([]domain.RetainResult, error) {
	f.calls++
	return nil, nil
}

func candidates(n int, content string) []domain.RecallItem {
	out := make([]domain.RecallItem, n)
	for i := range out {
		rank := i + 1
		out[i] = domain.RecallItem{Memory: domain.Memory{Content: content}, LexicalRank: &rank}
	}
	return out
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

func TestRecallRejectsEmbeddingOnly(t *testing.T) {
	store := &fakeStore{}
	e := &domain.Embedding{Model: "m", Vector: make([]float32, domain.EmbeddingDimensions)}
	_, err := newService(t, store).Recall(context.Background(), uuid.New(), "u", domain.RecallInput{QueryEmbedding: e})
	if !errors.Is(err, domain.ErrValidation) || store.calls != 0 {
		t.Fatalf("err = %v, store calls = %d", err, store.calls)
	}
}

func TestRetainValidatesBeforeStore(t *testing.T) {
	store := &fakeStore{}
	_, err := newService(t, store).Retain(context.Background(), uuid.New(), "u", domain.MemoryInput{})
	if !errors.Is(err, domain.ErrValidation) || store.calls != 0 {
		t.Fatalf("err = %v, store calls = %d", err, store.calls)
	}
}

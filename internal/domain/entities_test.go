package domain_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

func TestValidationErrorMatchesCategory(t *testing.T) {
	_, err := domain.MemoryInput{}.Prepare(retainInstant)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if !errors.Is(domain.ErrSpaceNotFound, domain.ErrNotFound) || !errors.Is(domain.ErrMemoryNotFound, domain.ErrNotFound) {
		t.Fatal("specific not-found errors must match ErrNotFound")
	}
}

func TestMemoryRejectsNonFiniteVector(t *testing.T) {
	e := embedding(domain.EmbeddingDimensions, "modelo-a")
	e.Vector[10] = float32(math.NaN())
	_, err := domain.MemoryInput{Content: "x", Embedding: e}.Prepare(retainInstant)
	assertField(t, err, "embedding.vector")
}

func TestMemoryRejectsLargeMetadata(t *testing.T) {
	md := domain.Metadata{"nota": strings.Repeat("a", domain.MaxMetadataBytes)}
	_, err := domain.MemoryInput{Content: "x", Metadata: md}.Prepare(retainInstant)
	assertField(t, err, "metadata")
}

func TestMemoryReportsEveryInvalidField(t *testing.T) {
	imp := float32(2)
	_, err := domain.MemoryInput{MemoryType: "x", Importance: &imp}.Prepare(retainInstant)
	for _, f := range []string{"memory_type", "content", "importance"} {
		assertField(t, err, f)
	}
}

func TestIdempotencyHashIgnoresMetadataKeyOrder(t *testing.T) {
	key := "evento-1"
	a, err := domain.MemoryInput{Content: "x", IdempotencyKey: &key, Metadata: domain.Metadata{"a": 1, "b": 2}}.Prepare(retainInstant)
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.MemoryInput{Content: "x", IdempotencyKey: &key, Metadata: domain.Metadata{"b": 2, "a": 1}}.Prepare(retainInstant)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.CheckReplay(domain.Memory{IdempotencyHash: a.IdempotencyHash}); err != nil {
		t.Fatalf("same request must replay: %v", err)
	}
}

func TestIdempotencyHashOnlyWithKey(t *testing.T) {
	m, err := domain.MemoryInput{Content: "x"}.Prepare(retainInstant)
	if err != nil {
		t.Fatal(err)
	}
	if m.IdempotencyHash != nil {
		t.Fatal("hash must be nil without idempotency_key")
	}
}

func TestMemoryActive(t *testing.T) {
	past, future := retainInstant.Add(-time.Minute), retainInstant.Add(time.Minute)
	cases := map[string]struct {
		m    domain.Memory
		want bool
	}{
		"ativa":    {domain.Memory{}, true},
		"expira":   {domain.Memory{ExpiresAt: &future}, true},
		"expirada": {domain.Memory{ExpiresAt: &past}, false},
		"excluída": {domain.Memory{DeletedAt: &past}, false},
	}
	for name, c := range cases {
		if got := c.m.Active(retainInstant); got != c.want {
			t.Errorf("%s: want %v, got %v", name, c.want, got)
		}
	}
}

func TestRecallItemMatchedBy(t *testing.T) {
	one := 1
	got := domain.RecallItem{LexicalRank: &one, SemanticRank: &one}.MatchedBy()
	if len(got) != 2 || got[0] != domain.MatchLexical || got[1] != domain.MatchSemantic {
		t.Fatalf("got %v", got)
	}
	if got := (domain.RecallItem{}).MatchedBy(); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func assertField(t *testing.T, err error, field string) {
	t.Helper()
	if e := (&entidades{err: err}).failedOnField(field); e != nil {
		t.Fatal(e)
	}
}

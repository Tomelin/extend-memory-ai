package domain

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// PatchField is one member of a JSON merge patch (RFC 7386): absent
// (Set=false), null (Set and Null) or a value.
type PatchField[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// ImmutableMemoryFields cannot change after retain; to correct them, a new
// memory is stored and related with "supersedes".
var ImmutableMemoryFields = []string{"memory_type", "occurred_at", "source", "idempotency_key"}

// MemoryPatch is a client merge patch of a memory. Immutable lists the
// immutable fields present in the request.
type MemoryPatch struct {
	Content    PatchField[string]
	Summary    PatchField[string]
	Embedding  PatchField[Embedding]
	Importance PatchField[float32]
	ExpiresAt  PatchField[time.Time]
	Metadata   PatchField[Metadata]
	Immutable  []string
}

// EmbeddingChange says what happens to the stored vector.
type EmbeddingChange int

const (
	EmbeddingKeep EmbeddingChange = iota
	EmbeddingReplace
	EmbeddingClear
)

// MemoryUpdate is the full new state of the mutable fields.
type MemoryUpdate struct {
	Content    string
	Summary    *string
	Importance float32
	ExpiresAt  *time.Time
	Metadata   Metadata
	Embedding  EmbeddingChange
	NewVector  *Embedding // set when Embedding is EmbeddingReplace
}

// Apply validates the patch against the current memory and returns its new
// state. When content changes without a new embedding, the old vector no
// longer represents the memory and is cleared.
func (p MemoryPatch) Apply(m Memory) (MemoryUpdate, error) {
	var v fieldErrors
	for _, f := range p.Immutable {
		v.add(f, "é imutável; grave uma nova memória e relacione com supersedes")
	}
	u := MemoryUpdate{
		Content: m.Content, Summary: m.Summary, Importance: m.Importance,
		ExpiresAt: m.ExpiresAt, Metadata: m.Metadata.orEmpty(),
	}

	if f := p.Content; f.Set {
		switch {
		case f.Null || strings.TrimSpace(f.Value) == "":
			v.add("content", "é obrigatório")
		case utf8.RuneCountInString(f.Value) > MaxContentLength:
			v.add("content", "excede %d caracteres", MaxContentLength)
		}
		u.Content = f.Value
	}
	if f := p.Summary; f.Set {
		u.Summary = nil
		if !f.Null {
			s := f.Value
			u.Summary = &s
		}
	}
	switch f := p.Embedding; {
	case f.Set && f.Null:
		u.Embedding = EmbeddingClear
	case f.Set:
		e := f.Value
		validateEmbedding(&v, "embedding", &e)
		u.Embedding, u.NewVector = EmbeddingReplace, &e
	case u.Content != m.Content && m.EmbeddingModel != nil:
		u.Embedding = EmbeddingClear
	}
	if f := p.Importance; f.Set {
		u.Importance = f.Value
		if f.Null || math.IsNaN(float64(f.Value)) || f.Value < 0 || f.Value > 1 {
			v.add("importance", "deve estar entre 0 e 1")
		}
	}
	if f := p.ExpiresAt; f.Set {
		u.ExpiresAt = nil
		if !f.Null {
			exp := f.Value.UTC()
			u.ExpiresAt = &exp
			if !exp.After(m.OccurredAt) {
				v.add("expires_at", "deve ser posterior a occurred_at")
			}
		}
	}
	if f := p.Metadata; f.Set {
		if f.Null {
			u.Metadata = Metadata{}
		} else {
			u.Metadata = mergePatch(u.Metadata, f.Value)
		}
		validateMetadata(&v, "metadata", u.Metadata)
	}
	return u, v.err()
}

// mergePatch applies RFC 7386 to an object: null members are removed and
// nested objects are merged recursively.
func mergePatch(target, patch map[string]any) map[string]any {
	out := make(map[string]any, len(target)+len(patch))
	for k, v := range target {
		out[k] = v
	}
	for k, pv := range patch {
		if pv == nil {
			delete(out, k)
			continue
		}
		if pm, ok := asObject(pv); ok {
			tm, _ := asObject(out[k])
			out[k] = mergePatch(tm, pm)
			continue
		}
		out[k] = pv
	}
	return out
}

func asObject(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case Metadata:
		return m, true
	}
	return nil, false
}

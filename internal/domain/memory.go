package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MemoryType classifies an atomic memory.
type MemoryType string

const (
	MemoryTypeFact        MemoryType = "fact"
	MemoryTypeExperience  MemoryType = "experience"
	MemoryTypePreference  MemoryType = "preference"
	MemoryTypeDecision    MemoryType = "decision"
	MemoryTypeObservation MemoryType = "observation"
	MemoryTypeSummary     MemoryType = "summary"
	MemoryTypeContext     MemoryType = "context"
)

// AllMemoryTypes lists every accepted memory type.
var AllMemoryTypes = []MemoryType{
	MemoryTypeFact, MemoryTypeExperience, MemoryTypePreference, MemoryTypeDecision,
	MemoryTypeObservation, MemoryTypeSummary, MemoryTypeContext,
}

// Valid reports whether t is an accepted memory type.
func (t MemoryType) Valid() bool {
	switch t {
	case MemoryTypeFact, MemoryTypeExperience, MemoryTypePreference, MemoryTypeDecision,
		MemoryTypeObservation, MemoryTypeSummary, MemoryTypeContext:
		return true
	}
	return false
}

// Limits and defaults of a memory, mirroring api/openapi.yaml and the
// constraints of the memories table.
const (
	DefaultMemoryType       = MemoryTypeFact
	DefaultImportance       = float32(0.5)
	MaxContentLength        = 16384 // characters
	MaxIdempotencyKeyLength = 255
	MaxSourceTypeLength     = 50
	EmbeddingDimensions     = 1536
	MaxBatchSize            = 100
)

// Source records where a memory came from, for traceability.
type Source struct {
	Type string
	Ref  string
}

func validateSource(v *fieldErrors, s Source) {
	if utf8.RuneCountInString(s.Type) > MaxSourceTypeLength {
		v.add("source.type", "excede %d caracteres", MaxSourceTypeLength)
	}
}

// Embedding is a vector produced by an external model. Vectors of different
// models are never compared.
type Embedding struct {
	Model  string
	Vector []float32
}

func validateEmbedding(v *fieldErrors, field string, e *Embedding) {
	if e == nil {
		return
	}
	if strings.TrimSpace(e.Model) == "" {
		v.add(field+".model", "é obrigatório")
	}
	if len(e.Vector) != EmbeddingDimensions {
		v.add(field+".vector", "esperado %d dimensões, recebido %d", EmbeddingDimensions, len(e.Vector))
		return
	}
	for i, x := range e.Vector {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			v.add(field+".vector", "posição %d não é um número finito", i)
			return
		}
	}
}

// MemoryInput is a client request to retain a memory. Nil pointers mean the
// field was omitted and its default applies.
type MemoryInput struct {
	MemoryType     MemoryType
	Content        string
	Summary        *string
	Embedding      *Embedding
	Source         Source
	IdempotencyKey *string
	Importance     *float32
	OccurredAt     *time.Time
	ExpiresAt      *time.Time
	Metadata       Metadata
}

// NewMemory is a validated MemoryInput with defaults resolved, ready to be
// stored.
type NewMemory struct {
	MemoryType      MemoryType
	Content         string
	Summary         *string
	Embedding       *Embedding
	Source          Source
	IdempotencyKey  *string
	IdempotencyHash []byte // set only when IdempotencyKey is present
	Importance      float32
	OccurredAt      time.Time
	ExpiresAt       *time.Time
	Metadata        Metadata
}

// Prepare validates the input and resolves defaults. now is the retain
// instant, used when OccurredAt is omitted.
func (in MemoryInput) Prepare(now time.Time) (NewMemory, error) {
	var v fieldErrors
	m := in.prepare(now, &v)
	return m, v.err()
}

// prepare records validation failures in v and returns the zero value when
// there is any.
func (in MemoryInput) prepare(now time.Time, v *fieldErrors) NewMemory {
	failures := len(v.list)
	m := NewMemory{
		MemoryType:     in.MemoryType,
		Content:        in.Content,
		Summary:        in.Summary,
		Embedding:      in.Embedding,
		Source:         in.Source,
		IdempotencyKey: in.IdempotencyKey,
		Importance:     DefaultImportance,
		OccurredAt:     now.UTC(),
		ExpiresAt:      in.ExpiresAt,
		Metadata:       in.Metadata.orEmpty(),
	}

	if m.MemoryType == "" {
		m.MemoryType = DefaultMemoryType
	} else if !m.MemoryType.Valid() {
		v.add("memory_type", "valor %q não é aceito", m.MemoryType)
	}
	switch {
	case strings.TrimSpace(m.Content) == "":
		v.add("content", "é obrigatório")
	case utf8.RuneCountInString(m.Content) > MaxContentLength:
		v.add("content", "excede %d caracteres", MaxContentLength)
	}
	validateEmbedding(v, "embedding", m.Embedding)
	validateSource(v, m.Source)
	if k := m.IdempotencyKey; k != nil {
		switch {
		case strings.TrimSpace(*k) == "":
			v.add("idempotency_key", "não pode ser vazia")
		case utf8.RuneCountInString(*k) > MaxIdempotencyKeyLength:
			v.add("idempotency_key", "excede %d caracteres", MaxIdempotencyKeyLength)
		}
	}
	if in.Importance != nil {
		m.Importance = *in.Importance
		if math.IsNaN(float64(m.Importance)) || m.Importance < 0 || m.Importance > 1 {
			v.add("importance", "deve estar entre 0 e 1")
		}
	}
	if in.OccurredAt != nil {
		m.OccurredAt = in.OccurredAt.UTC()
	}
	if m.ExpiresAt != nil {
		exp := m.ExpiresAt.UTC()
		m.ExpiresAt = &exp
		if !exp.After(m.OccurredAt) {
			v.add("expires_at", "deve ser posterior a occurred_at")
		}
	}
	validateMetadata(v, "metadata", m.Metadata)

	if len(v.list) > failures {
		return NewMemory{}
	}
	if m.IdempotencyKey != nil {
		m.IdempotencyHash = in.idempotencyHash(m)
	}
	return m
}

// idempotencyHash is the sha256 of the canonical form of the request. It
// excludes the embedding, which may vary by rounding, and uses occurred_at
// only when the client sent it, so a retry without occurred_at still matches.
func (in MemoryInput) idempotencyHash(m NewMemory) []byte {
	canonical := struct {
		MemoryType MemoryType `json:"memory_type"`
		Content    string     `json:"content"`
		Summary    *string    `json:"summary"`
		SourceType string     `json:"source_type"`
		SourceRef  string     `json:"source_ref"`
		Importance float32    `json:"importance"`
		OccurredAt *time.Time `json:"occurred_at"`
		ExpiresAt  *time.Time `json:"expires_at"`
		Metadata   Metadata   `json:"metadata"`
	}{
		MemoryType: m.MemoryType,
		Content:    m.Content,
		Summary:    m.Summary,
		SourceType: m.Source.Type,
		SourceRef:  m.Source.Ref,
		Importance: m.Importance,
		ExpiresAt:  m.ExpiresAt,
		Metadata:   m.Metadata,
	}
	if in.OccurredAt != nil {
		canonical.OccurredAt = &m.OccurredAt
	}
	// Map keys are sorted by encoding/json, so the output is deterministic.
	raw, err := json.Marshal(canonical)
	if err != nil {
		panic(fmt.Sprintf("idempotency hash: %v", err)) // metadata was already marshaled by validation
	}
	sum := sha256.Sum256(raw)
	return sum[:]
}

// CheckReplay compares this request with the memory already stored under the
// same idempotency key. It returns ErrIdempotencyConflict when the content
// differs.
func (m NewMemory) CheckReplay(existing Memory) error {
	if !bytes.Equal(m.IdempotencyHash, existing.IdempotencyHash) {
		return ErrIdempotencyConflict
	}
	return nil
}

// PrepareBatch validates a batch as a whole: any invalid item rejects the
// batch. Field errors are prefixed with "items[i]".
func PrepareBatch(items []MemoryInput, now time.Time) ([]NewMemory, error) {
	var v fieldErrors
	if len(items) == 0 {
		v.add("items", "deve conter ao menos 1 item")
		return nil, v.err()
	}
	if len(items) > MaxBatchSize {
		v.add("items", "excede %d itens (recebido %d)", MaxBatchSize, len(items))
		return nil, v.err()
	}
	out := make([]NewMemory, len(items))
	for i, in := range items {
		v.prefix = fmt.Sprintf("items[%d]", i)
		out[i] = in.prepare(now, &v)
	}
	if err := v.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Memory is a stored atomic memory. The vector is not kept here; only the
// model that produced it.
type Memory struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	SpaceID         uuid.UUID
	SpaceExternalID string
	MemoryType      MemoryType
	Content         string
	Summary         *string
	EmbeddingModel  *string
	Source          Source
	IdempotencyKey  *string
	IdempotencyHash []byte
	Importance      float32
	OccurredAt      time.Time
	ExpiresAt       *time.Time
	Metadata        Metadata
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// Active reports whether the memory can be listed and recalled at now.
func (m Memory) Active(now time.Time) bool {
	return m.DeletedAt == nil && (m.ExpiresAt == nil || m.ExpiresAt.After(now))
}

// RetainResult is the outcome of a retain. Created is false for an
// idempotent replay.
type RetainResult struct {
	Created bool
	Memory  Memory
}

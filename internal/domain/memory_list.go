package domain

import (
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits of a memory listing, mirroring api/openapi.yaml.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// MemoryCursor is the keyset position after which the next page starts.
type MemoryCursor struct {
	OccurredAt time.Time
	ID         uuid.UUID
}

// ListMemoriesInput is a client listing request. Nil pointers mean the field
// was omitted and its default applies.
type ListMemoriesInput struct {
	MemoryTypes    []MemoryType
	SourceType     string
	SourceRef      string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	Limit          *int
	After          *MemoryCursor
}

// ListMemoriesQuery is a validated ListMemoriesInput with defaults resolved.
type ListMemoriesQuery struct {
	MemoryTypes    []MemoryType
	SourceType     string
	SourceRef      string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	Limit          int
	After          *MemoryCursor
}

// Prepare validates the input and resolves defaults.
func (in ListMemoriesInput) Prepare() (ListMemoriesQuery, error) {
	var v fieldErrors
	q := ListMemoriesQuery{
		MemoryTypes:    in.MemoryTypes,
		SourceType:     in.SourceType,
		SourceRef:      in.SourceRef,
		OccurredAfter:  in.OccurredAfter,
		OccurredBefore: in.OccurredBefore,
		Limit:          DefaultListLimit,
		After:          in.After,
	}
	for _, t := range in.MemoryTypes {
		if !t.Valid() {
			v.add("memory_type", "valor %q não é aceito", t)
		}
	}
	if utf8.RuneCountInString(in.SourceType) > MaxSourceTypeLength {
		v.add("source_type", "excede %d caracteres", MaxSourceTypeLength)
	}
	if in.OccurredAfter != nil && in.OccurredBefore != nil && !in.OccurredBefore.After(*in.OccurredAfter) {
		v.add("occurred_before", "deve ser posterior a occurred_after")
	}
	if in.Limit != nil {
		q.Limit = *in.Limit
		if q.Limit < 1 || q.Limit > MaxListLimit {
			v.add("limit", "deve estar entre 1 e %d", MaxListLimit)
		}
	}
	return q, v.err()
}

// MemoryPage is one page of a listing. Next is nil on the last page.
type MemoryPage struct {
	Items []Memory
	Next  *MemoryCursor
}

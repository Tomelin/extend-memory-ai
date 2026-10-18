package domain

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// RelationType classifies an explicit relation between two memories.
type RelationType string

const (
	RelationRelatedTo   RelationType = "related_to"
	RelationSupports    RelationType = "supports"
	RelationContradicts RelationType = "contradicts"
	RelationSupersedes  RelationType = "supersedes"
	RelationDerivedFrom RelationType = "derived_from"
)

// AllRelationTypes lists every accepted relation type.
var AllRelationTypes = []RelationType{
	RelationRelatedTo, RelationSupports, RelationContradicts, RelationSupersedes, RelationDerivedFrom,
}

// Valid reports whether t is an accepted relation type.
func (t RelationType) Valid() bool {
	switch t {
	case RelationRelatedTo, RelationSupports, RelationContradicts, RelationSupersedes, RelationDerivedFrom:
		return true
	}
	return false
}

// DefaultLinkWeight is used when the client does not send a weight.
const DefaultLinkWeight = float32(1)

// Link is a directed relation between two memories of the same space.
type Link struct {
	SourceMemoryID uuid.UUID
	TargetMemoryID uuid.UUID
	RelationType   RelationType
	Weight         float32
	Metadata       Metadata
	CreatedAt      time.Time
}

// LinkInput is a client request to relate SourceMemoryID (from the URL) to
// TargetMemoryID.
type LinkInput struct {
	SourceMemoryID uuid.UUID
	TargetMemoryID uuid.UUID
	RelationType   RelationType
	Weight         *float32
	Metadata       Metadata
}

// NewLink is a validated LinkInput with defaults resolved.
type NewLink struct {
	SourceMemoryID uuid.UUID
	TargetMemoryID uuid.UUID
	RelationType   RelationType
	Weight         float32
	Metadata       Metadata
}

// Prepare validates the input and resolves defaults.
func (in LinkInput) Prepare() (NewLink, error) {
	var v fieldErrors
	l := NewLink{
		SourceMemoryID: in.SourceMemoryID,
		TargetMemoryID: in.TargetMemoryID,
		RelationType:   in.RelationType,
		Weight:         DefaultLinkWeight,
		Metadata:       in.Metadata.orEmpty(),
	}
	switch in.TargetMemoryID {
	case uuid.Nil:
		v.add("target_memory_id", "é obrigatório")
	case in.SourceMemoryID:
		v.add("target_memory_id", "deve ser diferente da memória de origem")
	}
	if !in.RelationType.Valid() {
		v.add("relation_type", "valor %q não é aceito", in.RelationType)
	}
	if in.Weight != nil {
		l.Weight = *in.Weight
		if math.IsNaN(float64(l.Weight)) || l.Weight < 0 || l.Weight > 1 {
			v.add("weight", "deve estar entre 0 e 1")
		}
	}
	validateMetadata(&v, "metadata", l.Metadata)
	return l, v.err()
}

// LinkDirection selects which relations of a memory are listed.
type LinkDirection string

const (
	LinkOutgoing LinkDirection = "outgoing"
	LinkIncoming LinkDirection = "incoming"
	LinkBoth     LinkDirection = "both"
)

// LinkFilter is a client request to list the relations of a memory.
type LinkFilter struct {
	Direction    LinkDirection
	RelationType RelationType // empty means any
}

// Prepare validates the filter and resolves the default direction (both).
func (f LinkFilter) Prepare() (LinkFilter, error) {
	var v fieldErrors
	switch f.Direction {
	case "":
		f.Direction = LinkBoth
	case LinkOutgoing, LinkIncoming, LinkBoth:
	default:
		v.add("direction", "valor %q não é aceito", f.Direction)
	}
	if f.RelationType != "" && !f.RelationType.Valid() {
		v.add("relation_type", "valor %q não é aceito", f.RelationType)
	}
	return f, v.err()
}

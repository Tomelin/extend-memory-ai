package memory

import (
	"encoding/json"
	"errors"
	"time"
)

// Metadata contains application-specific attributes associated with a resource.
type Metadata map[string]any

// Ptr returns a pointer to v. It is useful for optional scalar input fields.
func Ptr[T any](v T) *T { return &v }

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

type SpaceKind string

const (
	SpaceKindUser    SpaceKind = "user"
	SpaceKindAgent   SpaceKind = "agent"
	SpaceKindTeam    SpaceKind = "team"
	SpaceKindSession SpaceKind = "session"
	SpaceKindCustom  SpaceKind = "custom"
)

type RelationType string

const (
	RelationTypeRelatedTo   RelationType = "related_to"
	RelationTypeSupports    RelationType = "supports"
	RelationTypeContradicts RelationType = "contradicts"
	RelationTypeSupersedes  RelationType = "supersedes"
	RelationTypeDerivedFrom RelationType = "derived_from"
)

type LinkDirection string

const (
	LinkDirectionOutgoing LinkDirection = "outgoing"
	LinkDirectionIncoming LinkDirection = "incoming"
	LinkDirectionBoth     LinkDirection = "both"
)

type MatchStrategy string

const (
	MatchStrategyLexical  MatchStrategy = "lexical"
	MatchStrategySemantic MatchStrategy = "semantic"
)

type Project struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type SpaceInput struct {
	Name     string     `json:"name"`
	Kind     *SpaceKind `json:"kind,omitempty"`
	Metadata Metadata   `json:"metadata,omitempty"`
}

type Space struct {
	ID         string    `json:"id"`
	ExternalID string    `json:"external_id"`
	Name       string    `json:"name"`
	Kind       SpaceKind `json:"kind"`
	Metadata   Metadata  `json:"metadata"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SpacePage struct {
	Items      []Space `json:"items"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

type Embedding struct {
	Model  string    `json:"model"`
	Vector []float32 `json:"vector"`
}

type Source struct {
	Type string `json:"type,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type MemoryInput struct {
	MemoryType     *MemoryType `json:"memory_type,omitempty"`
	Content        string      `json:"content"`
	Summary        *string     `json:"summary,omitempty"`
	Embedding      *Embedding  `json:"embedding,omitempty"`
	Source         *Source     `json:"source,omitempty"`
	IdempotencyKey *string     `json:"idempotency_key,omitempty"`
	Importance     *float32    `json:"importance,omitempty"`
	OccurredAt     *time.Time  `json:"occurred_at,omitempty"`
	ExpiresAt      *time.Time  `json:"expires_at,omitempty"`
	Metadata       Metadata    `json:"metadata,omitempty"`
}

type Memory struct {
	ID              string     `json:"id"`
	SpaceExternalID string     `json:"space_external_id"`
	MemoryType      MemoryType `json:"memory_type"`
	Content         string     `json:"content"`
	Summary         *string    `json:"summary,omitempty"`
	EmbeddingModel  *string    `json:"embedding_model,omitempty"`
	Source          Source     `json:"source"`
	IdempotencyKey  *string    `json:"idempotency_key,omitempty"`
	Importance      float32    `json:"importance"`
	OccurredAt      time.Time  `json:"occurred_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Metadata        Metadata   `json:"metadata"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type RetainResult struct {
	Created bool   `json:"created"`
	Memory  Memory `json:"memory"`
}

type MemoryPage struct {
	Items      []Memory `json:"items"`
	NextCursor *string  `json:"next_cursor,omitempty"`
}

type RecallFilters struct {
	MemoryTypes    []MemoryType `json:"memory_types,omitempty"`
	OccurredAfter  *time.Time   `json:"occurred_after,omitempty"`
	OccurredBefore *time.Time   `json:"occurred_before,omitempty"`
	SourceType     string       `json:"source_type,omitempty"`
	SourceRef      string       `json:"source_ref,omitempty"`
	Metadata       Metadata     `json:"metadata,omitempty"`
}

type RecallRequest struct {
	Query          string         `json:"query,omitempty"`
	QueryEmbedding *Embedding     `json:"query_embedding,omitempty"`
	Filters        *RecallFilters `json:"filters,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	CandidateLimit int            `json:"candidate_limit,omitempty"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	IncludeLinks   bool           `json:"include_links,omitempty"`
}

type RecallItem struct {
	Memory       Memory          `json:"memory"`
	Score        float32         `json:"score"`
	MatchedBy    []MatchStrategy `json:"matched_by"`
	LexicalRank  *int            `json:"lexical_rank,omitempty"`
	SemanticRank *int            `json:"semantic_rank,omitempty"`
	Links        []Link          `json:"links,omitempty"`
}

type RecallResponse struct {
	Items         []RecallItem `json:"items"`
	TokenEstimate int          `json:"token_estimate"`
	Truncated     bool         `json:"truncated"`
}

type LinkInput struct {
	TargetMemoryID string       `json:"target_memory_id"`
	RelationType   RelationType `json:"relation_type"`
	Weight         *float32     `json:"weight,omitempty"`
	Metadata       Metadata     `json:"metadata,omitempty"`
}

type Link struct {
	SourceMemoryID string       `json:"source_memory_id"`
	TargetMemoryID string       `json:"target_memory_id"`
	RelationType   RelationType `json:"relation_type"`
	Weight         float32      `json:"weight"`
	Metadata       Metadata     `json:"metadata"`
	CreatedAt      time.Time    `json:"created_at"`
}

// MemoryPatch represents a JSON Merge Patch. Clear fields explicitly encode
// JSON null, while nil value fields are omitted from the request.
type MemoryPatch struct {
	Content        *string
	Summary        *string
	ClearSummary   bool
	Embedding      *Embedding
	ClearEmbedding bool
	Importance     *float32
	ExpiresAt      *time.Time
	ClearExpiresAt bool
	Metadata       Metadata
	ClearMetadata  bool
}

func (p MemoryPatch) MarshalJSON() ([]byte, error) {
	if p.Summary != nil && p.ClearSummary {
		return nil, errors.New("memory: Summary and ClearSummary cannot be used together")
	}
	if p.Embedding != nil && p.ClearEmbedding {
		return nil, errors.New("memory: Embedding and ClearEmbedding cannot be used together")
	}
	if p.ExpiresAt != nil && p.ClearExpiresAt {
		return nil, errors.New("memory: ExpiresAt and ClearExpiresAt cannot be used together")
	}
	if p.Metadata != nil && p.ClearMetadata {
		return nil, errors.New("memory: Metadata and ClearMetadata cannot be used together")
	}

	fields := make(map[string]any, 6)
	if p.Content != nil {
		fields["content"] = *p.Content
	}
	if p.Summary != nil {
		fields["summary"] = *p.Summary
	} else if p.ClearSummary {
		fields["summary"] = nil
	}
	if p.Embedding != nil {
		fields["embedding"] = p.Embedding
	} else if p.ClearEmbedding {
		fields["embedding"] = nil
	}
	if p.Importance != nil {
		fields["importance"] = *p.Importance
	}
	if p.ExpiresAt != nil {
		fields["expires_at"] = p.ExpiresAt
	} else if p.ClearExpiresAt {
		fields["expires_at"] = nil
	}
	if p.Metadata != nil {
		fields["metadata"] = p.Metadata
	} else if p.ClearMetadata {
		fields["metadata"] = nil
	}
	return json.Marshal(fields)
}

package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
)

func toAPISpace(s domain.Space) api.Space {
	return api.Space{
		Id: s.ID, ExternalId: s.ExternalID, Name: s.Name, Kind: api.SpaceKind(s.Kind),
		Metadata: toAPIMetadata(s.Metadata), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func toAPIMemory(m domain.Memory) api.Memory {
	return api.Memory{
		Id:              m.ID,
		SpaceExternalId: m.SpaceExternalID,
		MemoryType:      api.MemoryType(m.MemoryType),
		Content:         m.Content,
		Summary:         m.Summary,
		EmbeddingModel:  m.EmbeddingModel,
		Source:          api.Source{Type: optional(m.Source.Type), Ref: optional(m.Source.Ref)},
		IdempotencyKey:  m.IdempotencyKey,
		Importance:      m.Importance,
		OccurredAt:      m.OccurredAt,
		ExpiresAt:       m.ExpiresAt,
		Metadata:        toAPIMetadata(m.Metadata),
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func toAPIRetainResult(r domain.RetainResult) api.RetainResult {
	return api.RetainResult{Created: r.Created, Memory: toAPIMemory(r.Memory)}
}

func toAPIRecallResponse(r domain.RecallResult) api.RecallResponse {
	items := make([]api.RecallItem, len(r.Items))
	for i, it := range r.Items {
		matched := it.MatchedBy()
		by := make([]api.RecallItemMatchedBy, len(matched))
		for j, m := range matched {
			by[j] = api.RecallItemMatchedBy(m)
		}
		items[i] = api.RecallItem{
			Memory:       toAPIMemory(it.Memory),
			Score:        float32(it.Score),
			MatchedBy:    by,
			LexicalRank:  it.LexicalRank,
			SemanticRank: it.SemanticRank,
		}
		if it.Links != nil {
			links := toAPILinks(it.Links)
			items[i].Links = &links
		}
	}
	return api.RecallResponse{Items: items, TokenEstimate: r.TokenEstimate, Truncated: r.Truncated}
}

func fromAPIMemoryInput(in api.MemoryInput) domain.MemoryInput {
	out := domain.MemoryInput{
		Content:        in.Content,
		Summary:        in.Summary,
		Embedding:      fromAPIEmbedding(in.Embedding),
		IdempotencyKey: in.IdempotencyKey,
		Importance:     in.Importance,
		OccurredAt:     in.OccurredAt,
		ExpiresAt:      in.ExpiresAt,
		Metadata:       fromAPIMetadata(in.Metadata),
	}
	if in.MemoryType != nil {
		out.MemoryType = domain.MemoryType(*in.MemoryType)
	}
	if in.Source != nil {
		out.Source = domain.Source{Type: deref(in.Source.Type), Ref: deref(in.Source.Ref)}
	}
	return out
}

func fromAPIEmbedding(e *api.Embedding) *domain.Embedding {
	if e == nil {
		return nil
	}
	return &domain.Embedding{Model: e.Model, Vector: e.Vector}
}

// recallRequest mirrors api.RecallRequest without its generated union
// unmarshaler, so that unknown fields are rejected.
type recallRequest struct {
	Query          *string            `json:"query"`
	QueryEmbedding *api.Embedding     `json:"query_embedding"`
	Filters        *api.RecallFilters `json:"filters"`
	Limit          *int               `json:"limit"`
	CandidateLimit *int               `json:"candidate_limit"`
	MaxTokens      *int               `json:"max_tokens"`
	IncludeLinks   *bool              `json:"include_links"`
}

func (r recallRequest) toDomain() domain.RecallInput {
	in := domain.RecallInput{
		Query:          r.Query,
		QueryEmbedding: fromAPIEmbedding(r.QueryEmbedding),
		Limit:          r.Limit,
		CandidateLimit: r.CandidateLimit,
		MaxTokens:      r.MaxTokens,
		IncludeLinks:   r.IncludeLinks != nil && *r.IncludeLinks,
	}
	if f := r.Filters; f != nil {
		if f.MemoryTypes != nil {
			for _, t := range *f.MemoryTypes {
				in.Filters.MemoryTypes = append(in.Filters.MemoryTypes, domain.MemoryType(t))
			}
		}
		in.Filters.OccurredAfter = f.OccurredAfter
		in.Filters.OccurredBefore = f.OccurredBefore
		in.Filters.SourceType = deref(f.SourceType)
		in.Filters.SourceRef = deref(f.SourceRef)
		in.Filters.Metadata = fromAPIMetadata(f.Metadata)
	}
	return in
}

// cursor is the opaque next_cursor: base64url of a JSON keyset position.
type cursor struct {
	OccurredAt time.Time `json:"o"`
	ID         uuid.UUID `json:"i"`
}

func encodeCursor(c *domain.MemoryCursor) *string {
	if c == nil {
		return nil
	}
	raw, _ := json.Marshal(cursor{OccurredAt: c.OccurredAt, ID: c.ID})
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

var errInvalidCursor = errors.New("cursor inválido")

func decodeCursor(s string) (*domain.MemoryCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == uuid.Nil || c.OccurredAt.IsZero() {
		return nil, errInvalidCursor
	}
	return &domain.MemoryCursor{OccurredAt: c.OccurredAt, ID: c.ID}, nil
}

func toAPIMetadata(m domain.Metadata) api.Metadata {
	if m == nil {
		return api.Metadata{}
	}
	return api.Metadata(m)
}

func fromAPIMetadata(m *api.Metadata) domain.Metadata {
	if m == nil {
		return nil
	}
	return domain.Metadata(*m)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toAPILink(l domain.Link) api.Link {
	return api.Link{
		SourceMemoryId: l.SourceMemoryID,
		TargetMemoryId: l.TargetMemoryID,
		RelationType:   api.RelationType(l.RelationType),
		Weight:         l.Weight,
		Metadata:       toAPIMetadata(l.Metadata),
		CreatedAt:      l.CreatedAt,
	}
}

func toAPILinks(ls []domain.Link) []api.Link {
	out := make([]api.Link, len(ls))
	for i, l := range ls {
		out[i] = toAPILink(l)
	}
	return out
}

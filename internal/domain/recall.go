package domain

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MatchStrategy names the retrieval strategy that found a memory.
type MatchStrategy string

const (
	MatchLexical  MatchStrategy = "lexical"
	MatchSemantic MatchStrategy = "semantic"
)

// Limits and defaults of a recall, mirroring api/openapi.yaml.
const (
	DefaultRecallLimit    = 10
	MaxRecallLimit        = 100
	DefaultCandidateLimit = 50
	MaxCandidateLimit     = 200
	MaxQueryLength        = 2000 // characters
)

// RecallFilters restrict both retrieval strategies.
type RecallFilters struct {
	MemoryTypes    []MemoryType
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	SourceType     string
	SourceRef      string
	Metadata       Metadata // containment: metadata @> filter
}

// RecallInput is a client recall request. Nil pointers mean the field was
// omitted and its default applies.
type RecallInput struct {
	Query          *string
	QueryEmbedding *Embedding
	Filters        RecallFilters
	Limit          *int
	CandidateLimit *int
	MaxTokens      *int
	IncludeLinks   bool
}

// RecallQuery is a validated RecallInput with defaults resolved.
type RecallQuery struct {
	Query          string // empty when only QueryEmbedding is used
	QueryEmbedding *Embedding
	Filters        RecallFilters
	Limit          int
	CandidateLimit int
	MaxTokens      *int
	IncludeLinks   bool
}

// Prepare validates the input and resolves defaults.
func (in RecallInput) Prepare() (RecallQuery, error) {
	var v fieldErrors
	q := RecallQuery{
		QueryEmbedding: in.QueryEmbedding,
		Filters:        in.Filters,
		Limit:          DefaultRecallLimit,
		CandidateLimit: DefaultCandidateLimit,
		MaxTokens:      in.MaxTokens,
		IncludeLinks:   in.IncludeLinks,
	}

	if in.Query == nil && in.QueryEmbedding == nil {
		v.add("query", "informe query, query_embedding ou ambos")
	}
	if in.Query != nil {
		q.Query = *in.Query
		switch {
		case strings.TrimSpace(q.Query) == "":
			v.add("query", "não pode ser vazia")
		case utf8.RuneCountInString(q.Query) > MaxQueryLength:
			v.add("query", "excede %d caracteres", MaxQueryLength)
		}
	}
	validateEmbedding(&v, "query_embedding", in.QueryEmbedding)
	if in.Limit != nil {
		q.Limit = *in.Limit
		if q.Limit < 1 || q.Limit > MaxRecallLimit {
			v.add("limit", "deve estar entre 1 e %d", MaxRecallLimit)
		}
	}
	if in.CandidateLimit != nil {
		q.CandidateLimit = *in.CandidateLimit
		if q.CandidateLimit < 1 || q.CandidateLimit > MaxCandidateLimit {
			v.add("candidate_limit", "deve estar entre 1 e %d", MaxCandidateLimit)
		}
	}
	if in.MaxTokens != nil && *in.MaxTokens < 1 {
		v.add("max_tokens", "deve ser maior que zero")
	}
	validateRecallFilters(&v, in.Filters)
	return q, v.err()
}

func validateRecallFilters(v *fieldErrors, f RecallFilters) {
	for _, t := range f.MemoryTypes {
		if !t.Valid() {
			v.add("filters.memory_types", "valor %q não é aceito", t)
		}
	}
	if f.OccurredAfter != nil && f.OccurredBefore != nil && !f.OccurredBefore.After(*f.OccurredAfter) {
		v.add("filters.occurred_before", "deve ser posterior a occurred_after")
	}
	if utf8.RuneCountInString(f.SourceType) > MaxSourceTypeLength {
		v.add("filters.source_type", "excede %d caracteres", MaxSourceTypeLength)
	}
	if f.Metadata != nil {
		validateMetadata(v, "filters.metadata", f.Metadata)
	}
}

// Lexical reports whether the full-text strategy runs.
func (q RecallQuery) Lexical() bool { return q.Query != "" }

// Semantic reports whether the vector strategy runs.
func (q RecallQuery) Semantic() bool { return q.QueryEmbedding != nil }

// RecallItem is a ranked memory. Ranks are 1-based positions in each
// strategy's candidate list; nil means the strategy did not find it. Links
// holds the direct relations when include_links is requested.
type RecallItem struct {
	Memory       Memory
	Score        float64
	LexicalRank  *int
	SemanticRank *int
	Links        []Link
}

// RecallCandidates are the ranked lists of each strategy, before fusion.
type RecallCandidates struct {
	Lexical  []RecallItem
	Semantic []RecallItem
}

// RRFK is the Reciprocal Rank Fusion constant.
const RRFK = 60

// FuseRRF merges the candidate lists by memory and scores each one with
// Σ 1 / (RRFK + rank). Scores of different scales are never added. Ties are
// broken by the best rank and then by id, so the order is deterministic.
func FuseRRF(c RecallCandidates) []RecallItem {
	byID := map[uuid.UUID]*RecallItem{}
	var order []uuid.UUID
	merge := func(list []RecallItem) {
		for _, it := range list {
			cur, ok := byID[it.Memory.ID]
			if !ok {
				cp := it
				byID[it.Memory.ID] = &cp
				order = append(order, it.Memory.ID)
				continue
			}
			if it.LexicalRank != nil {
				cur.LexicalRank = it.LexicalRank
			}
			if it.SemanticRank != nil {
				cur.SemanticRank = it.SemanticRank
			}
		}
	}
	merge(c.Lexical)
	merge(c.Semantic)

	out := make([]RecallItem, 0, len(order))
	for _, id := range order {
		it := *byID[id]
		it.Score = 0
		for _, r := range []*int{it.LexicalRank, it.SemanticRank} {
			if r != nil {
				it.Score += 1.0 / float64(RRFK+*r)
			}
		}
		out = append(out, it)
	}
	slices.SortStableFunc(out, func(a, b RecallItem) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			cmp.Compare(bestRank(a), bestRank(b)),
			strings.Compare(a.Memory.ID.String(), b.Memory.ID.String()),
		)
	})
	return out
}

func bestRank(it RecallItem) int {
	best := int(^uint(0) >> 1)
	for _, r := range []*int{it.LexicalRank, it.SemanticRank} {
		if r != nil && *r < best {
			best = *r
		}
	}
	return best
}

// MatchedBy lists the strategies that found the memory.
func (i RecallItem) MatchedBy() []MatchStrategy {
	var s []MatchStrategy
	if i.LexicalRank != nil {
		s = append(s, MatchLexical)
	}
	if i.SemanticRank != nil {
		s = append(s, MatchSemantic)
	}
	return s
}

// RecallResult is the recall response. Truncated is true when the token
// budget discarded results.
type RecallResult struct {
	Items         []RecallItem
	TokenEstimate int
	Truncated     bool
}

package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// MemoryService is the use case port consumed by the space, memory and
// recall handlers.
type MemoryService interface {
	UpsertSpace(ctx context.Context, projectID uuid.UUID, in domain.SpaceInput) (domain.Space, bool, error)
	GetSpace(ctx context.Context, projectID uuid.UUID, externalID string) (domain.Space, error)
	DeleteSpace(ctx context.Context, projectID uuid.UUID, externalID string) error
	Retain(ctx context.Context, projectID uuid.UUID, externalID string, in domain.MemoryInput) (domain.RetainResult, error)
	RetainBatch(ctx context.Context, projectID uuid.UUID, externalID string, in []domain.MemoryInput) ([]domain.RetainResult, error)
	GetMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) (domain.Memory, error)
	DeleteMemory(ctx context.Context, projectID uuid.UUID, externalID string, id uuid.UUID) error
	ListMemories(ctx context.Context, projectID uuid.UUID, externalID string, in domain.ListMemoriesInput) (domain.MemoryPage, error)
	Recall(ctx context.Context, projectID uuid.UUID, externalID string, in domain.RecallInput) (domain.RecallResult, error)
}

func (h handlers) upsertSpace(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	var body api.SpaceInput
	if !decodeJSON(c, &body) {
		return
	}
	in := domain.SpaceInput{ExternalID: c.Param("space"), Name: body.Name, Metadata: fromAPIMetadata(body.Metadata)}
	if body.Kind != nil {
		in.Kind = domain.SpaceKind(*body.Kind)
	}
	sp, created, err := h.deps.Memories.UpsertSpace(c.Request.Context(), p.ProjectID, in)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, toAPISpace(sp))
}

func (h handlers) getSpace(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	sp, err := h.deps.Memories.GetSpace(c.Request.Context(), p.ProjectID, c.Param("space"))
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAPISpace(sp))
}

func (h handlers) deleteSpace(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	if err := h.deps.Memories.DeleteSpace(c.Request.Context(), p.ProjectID, c.Param("space")); err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h handlers) retainMemory(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	var body api.MemoryInput
	if !decodeJSON(c, &body) {
		return
	}
	space := c.Param("space")
	res, err := h.deps.Memories.Retain(c.Request.Context(), p.ProjectID, space, fromAPIMemoryInput(body))
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
		c.Header("Location", "/v1/spaces/"+url.PathEscape(space)+"/memories/"+res.Memory.ID.String())
	}
	c.JSON(status, toAPIRetainResult(res))
}

func (h handlers) retainMemoriesBatch(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	var body api.RetainMemoriesBatchJSONBody
	if !decodeJSON(c, &body) {
		return
	}
	in := make([]domain.MemoryInput, len(body.Items))
	for i, item := range body.Items {
		in[i] = fromAPIMemoryInput(item)
	}
	res, err := h.deps.Memories.RetainBatch(c.Request.Context(), p.ProjectID, c.Param("space"), in)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	items := make([]api.RetainResult, len(res))
	for i, r := range res {
		items[i] = toAPIRetainResult(r)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h handlers) listMemories(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	in, err := listMemoriesInput(c)
	if err != nil {
		problem.Abort(c, http.StatusBadRequest, api.ProblemCodeMalformedRequest, err.Error())
		return
	}
	page, err := h.deps.Memories.ListMemories(c.Request.Context(), p.ProjectID, c.Param("space"), in)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	items := make([]api.Memory, len(page.Items))
	for i, m := range page.Items {
		items[i] = toAPIMemory(m)
	}
	c.JSON(http.StatusOK, api.MemoryPage{Items: items, NextCursor: encodeCursor(page.Next)})
}

func listMemoriesInput(c *gin.Context) (domain.ListMemoriesInput, error) {
	in := domain.ListMemoriesInput{SourceType: c.Query("source_type"), SourceRef: c.Query("source_ref")}
	for _, t := range c.QueryArray("memory_type") {
		in.MemoryTypes = append(in.MemoryTypes, domain.MemoryType(t))
	}
	var err error
	if in.OccurredAfter, err = queryTime(c, "occurred_after"); err != nil {
		return in, err
	}
	if in.OccurredBefore, err = queryTime(c, "occurred_before"); err != nil {
		return in, err
	}
	if raw, ok := c.GetQuery("limit"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return in, errors.New("limit deve ser um número inteiro")
		}
		in.Limit = &n
	}
	if raw, ok := c.GetQuery("cursor"); ok {
		if in.After, err = decodeCursor(raw); err != nil {
			return in, err
		}
	}
	return in, nil
}

func queryTime(c *gin.Context, name string) (*time.Time, error) {
	raw, ok := c.GetQuery(name)
	if !ok {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errors.New(name + " deve estar no formato RFC 3339")
	}
	return &t, nil
}

func (h handlers) getMemory(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := memoryID(c)
	if !ok {
		return
	}
	m, err := h.deps.Memories.GetMemory(c.Request.Context(), p.ProjectID, c.Param("space"), id)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAPIMemory(m))
}

func (h handlers) deleteMemory(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := memoryID(c)
	if !ok {
		return
	}
	if err := h.deps.Memories.DeleteMemory(c.Request.Context(), p.ProjectID, c.Param("space"), id); err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h handlers) recall(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	var body recallRequest
	if !decodeJSON(c, &body) {
		return
	}
	res, err := h.deps.Memories.Recall(c.Request.Context(), p.ProjectID, c.Param("space"), body.toDomain())
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAPIRecallResponse(res))
}

func principal(c *gin.Context) (domain.Principal, bool) {
	p, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		problem.Abort(c, http.StatusUnauthorized, api.ProblemCodeUnauthenticated, "")
	}
	return p, ok
}

func memoryID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("memory_id"))
	if err != nil {
		problem.Abort(c, http.StatusBadRequest, api.ProblemCodeMalformedRequest, "memory_id deve ser um UUID")
		return uuid.Nil, false
	}
	return id, true
}

// decodeJSON reads exactly one JSON object, rejecting unknown fields
// (additionalProperties: false in the contract).
func decodeJSON(c *gin.Context, dst any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil && dec.More() {
		err = errors.New("o corpo deve conter um único objeto JSON")
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		problem.Abort(c, http.StatusRequestEntityTooLarge, api.ProblemCodePayloadTooLarge,
			"o corpo deve ter no máximo "+strconv.FormatInt(tooLarge.Limit, 10)+" bytes")
		return false
	}
	problem.Abort(c, http.StatusBadRequest, api.ProblemCodeMalformedRequest, "JSON inválido: "+err.Error())
	return false
}

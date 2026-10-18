package memory

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ListMemoriesOptions filters and paginates active memories.
type ListMemoriesOptions struct {
	Limit          int
	Cursor         string
	MemoryTypes    []MemoryType
	SourceType     string
	SourceRef      string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
}

// RetainMemory stores one atomic memory.
func (c *Client) RetainMemory(ctx context.Context, space string, input MemoryInput) (RetainResult, error) {
	ctx, span := startSpan(ctx, "RetainMemory")
	defer span.End()

	var result RetainResult
	endpoint := fmt.Sprintf("/v1/spaces/%s/memories", pathSegment(space))
	_, err := c.do(ctx, http.MethodPost, endpoint, nil, "application/json", input, &result)
	return result, err
}

// RetainMemories stores up to 100 memories in a single transaction.
func (c *Client) RetainMemories(ctx context.Context, space string, inputs []MemoryInput) ([]RetainResult, error) {
	ctx, span := startSpan(ctx, "RetainMemories")
	defer span.End()

	request := struct {
		Items []MemoryInput `json:"items"`
	}{Items: inputs}
	response := struct {
		Items []RetainResult `json:"items"`
	}{}
	endpoint := fmt.Sprintf("/v1/spaces/%s/memories/batch", pathSegment(space))
	_, err := c.do(ctx, http.MethodPost, endpoint, nil, "application/json", request, &response)
	return response.Items, err
}

// ListMemories returns one page of active memories, newest occurrence first.
func (c *Client) ListMemories(ctx context.Context, space string, options ListMemoriesOptions) (MemoryPage, error) {
	ctx, span := startSpan(ctx, "ListMemories")
	defer span.End()

	query := make(url.Values)
	setInt(query, "limit", options.Limit)
	if options.Cursor != "" {
		query.Set("cursor", options.Cursor)
	}
	for _, memoryType := range options.MemoryTypes {
		query.Add("memory_type", string(memoryType))
	}
	if options.SourceType != "" {
		query.Set("source_type", options.SourceType)
	}
	if options.SourceRef != "" {
		query.Set("source_ref", options.SourceRef)
	}
	setTime(query, "occurred_after", options.OccurredAfter)
	setTime(query, "occurred_before", options.OccurredBefore)

	var page MemoryPage
	endpoint := fmt.Sprintf("/v1/spaces/%s/memories", pathSegment(space))
	_, err := c.do(ctx, http.MethodGet, endpoint, query, "", nil, &page)
	return page, err
}

// GetMemory returns one active memory by ID.
func (c *Client) GetMemory(ctx context.Context, space, memoryID string) (Memory, error) {
	ctx, span := startSpan(ctx, "GetMemory")
	defer span.End()

	var result Memory
	endpoint := fmt.Sprintf("/v1/spaces/%s/memories/%s", pathSegment(space), pathSegment(memoryID))
	_, err := c.do(ctx, http.MethodGet, endpoint, nil, "", nil, &result)
	return result, err
}

// UpdateMemory changes mutable fields using JSON Merge Patch semantics.
func (c *Client) UpdateMemory(ctx context.Context, space, memoryID string, patch MemoryPatch) (Memory, error) {
	ctx, span := startSpan(ctx, "UpdateMemory")
	defer span.End()

	var result Memory
	endpoint := fmt.Sprintf("/v1/spaces/%s/memories/%s", pathSegment(space), pathSegment(memoryID))
	_, err := c.do(ctx, http.MethodPatch, endpoint, nil, "application/merge-patch+json", patch, &result)
	return result, err
}

// DeleteMemory logically deletes a memory.
func (c *Client) DeleteMemory(ctx context.Context, space, memoryID string) error {
	ctx, span := startSpan(ctx, "DeleteMemory")
	defer span.End()

	endpoint := fmt.Sprintf("/v1/spaces/%s/memories/%s", pathSegment(space), pathSegment(memoryID))
	_, err := c.do(ctx, http.MethodDelete, endpoint, nil, "", nil, nil)
	return err
}

// Recall runs hybrid, lexical, or semantic memory retrieval.
func (c *Client) Recall(ctx context.Context, space string, request RecallRequest) (RecallResponse, error) {
	ctx, span := startSpan(ctx, "Recall")
	defer span.End()

	var response RecallResponse
	endpoint := fmt.Sprintf("/v1/spaces/%s/recall", pathSegment(space))
	_, err := c.do(ctx, http.MethodPost, endpoint, nil, "application/json", request, &response)
	return response, err
}

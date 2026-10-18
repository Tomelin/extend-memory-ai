package memory

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ListSpacesOptions filters and paginates spaces.
type ListSpacesOptions struct {
	Limit  int
	Cursor string
	Kind   SpaceKind
}

// GetCurrentProject returns the project associated with the API key.
func (c *Client) GetCurrentProject(ctx context.Context) (Project, error) {
	ctx, span := startSpan(ctx, "GetCurrentProject")
	defer span.End()

	var project Project
	_, err := c.do(ctx, http.MethodGet, "/v1/project", nil, "", nil, &project)
	return project, err
}

// ListSpaces returns one page of the project's spaces.
func (c *Client) ListSpaces(ctx context.Context, options ListSpacesOptions) (SpacePage, error) {
	ctx, span := startSpan(ctx, "ListSpaces")
	defer span.End()

	query := make(url.Values)
	setInt(query, "limit", options.Limit)
	if options.Cursor != "" {
		query.Set("cursor", options.Cursor)
	}
	if options.Kind != "" {
		query.Set("kind", string(options.Kind))
	}
	var page SpacePage
	_, err := c.do(ctx, http.MethodGet, "/v1/spaces", query, "", nil, &page)
	return page, err
}

// UpsertSpace creates or replaces a space. created is true when the server
// responds with HTTP 201.
func (c *Client) UpsertSpace(ctx context.Context, externalID string, input SpaceInput) (space Space, created bool, err error) {
	ctx, span := startSpan(ctx, "UpsertSpace")
	defer span.End()

	endpoint := fmt.Sprintf("/v1/spaces/%s", pathSegment(externalID))
	status, err := c.do(ctx, http.MethodPut, endpoint, nil, "application/json", input, &space)
	return space, status == http.StatusCreated, err
}

// GetSpace returns a space by external ID.
func (c *Client) GetSpace(ctx context.Context, externalID string) (Space, error) {
	ctx, span := startSpan(ctx, "GetSpace")
	defer span.End()

	var space Space
	endpoint := fmt.Sprintf("/v1/spaces/%s", pathSegment(externalID))
	_, err := c.do(ctx, http.MethodGet, endpoint, nil, "", nil, &space)
	return space, err
}

// DeleteSpace permanently removes a space and all of its memories.
func (c *Client) DeleteSpace(ctx context.Context, externalID string) error {
	ctx, span := startSpan(ctx, "DeleteSpace")
	defer span.End()

	endpoint := fmt.Sprintf("/v1/spaces/%s", pathSegment(externalID))
	_, err := c.do(ctx, http.MethodDelete, endpoint, nil, "", nil, nil)
	return err
}

// Liveness checks whether the API process is running.
func (c *Client) Liveness(ctx context.Context) error {
	ctx, span := startSpan(ctx, "Liveness")
	defer span.End()

	_, err := c.do(ctx, http.MethodGet, "/healthz", nil, "", nil, nil)
	return err
}

// Readiness checks whether the API and its database are ready.
func (c *Client) Readiness(ctx context.Context) error {
	ctx, span := startSpan(ctx, "Readiness")
	defer span.End()

	_, err := c.do(ctx, http.MethodGet, "/readyz", nil, "", nil, nil)
	return err
}

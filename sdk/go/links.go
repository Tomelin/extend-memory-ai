package memory

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ListLinksOptions filters links by direction and relation type.
type ListLinksOptions struct {
	Direction    LinkDirection
	RelationType RelationType
}

// CreateLink creates an explicit relationship from one memory to another.
func (c *Client) CreateLink(ctx context.Context, space, sourceMemoryID string, input LinkInput) (Link, error) {
	ctx, span := startSpan(ctx, "CreateLink")
	defer span.End()

	var link Link
	endpoint := fmt.Sprintf(
		"/v1/spaces/%s/memories/%s/links",
		pathSegment(space),
		pathSegment(sourceMemoryID),
	)
	_, err := c.do(ctx, http.MethodPost, endpoint, nil, "application/json", input, &link)
	return link, err
}

// ListLinks lists incoming, outgoing, or all links for a memory.
func (c *Client) ListLinks(ctx context.Context, space, memoryID string, options ListLinksOptions) ([]Link, error) {
	ctx, span := startSpan(ctx, "ListLinks")
	defer span.End()

	query := make(url.Values)
	if options.Direction != "" {
		query.Set("direction", string(options.Direction))
	}
	if options.RelationType != "" {
		query.Set("relation_type", string(options.RelationType))
	}
	response := struct {
		Items []Link `json:"items"`
	}{}
	endpoint := fmt.Sprintf(
		"/v1/spaces/%s/memories/%s/links",
		pathSegment(space),
		pathSegment(memoryID),
	)
	_, err := c.do(ctx, http.MethodGet, endpoint, query, "", nil, &response)
	return response.Items, err
}

// DeleteLink removes an explicit relationship.
func (c *Client) DeleteLink(ctx context.Context, space, sourceMemoryID, targetMemoryID string, relationType RelationType) error {
	ctx, span := startSpan(ctx, "DeleteLink")
	defer span.End()

	endpoint := fmt.Sprintf(
		"/v1/spaces/%s/memories/%s/links/%s/%s",
		pathSegment(space),
		pathSegment(sourceMemoryID),
		pathSegment(targetMemoryID),
		pathSegment(string(relationType)),
	)
	_, err := c.do(ctx, http.MethodDelete, endpoint, nil, "", nil, nil)
	return err
}

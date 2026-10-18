package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultUserAgent = "extend-memory-ai-go/1.0"
	maxErrorBody     = 1 << 20
)

// Client calls an extend-memory-ai server. A Client is safe for concurrent use.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// Option customizes a Client.
type Option func(*Client) error

// WithHTTPClient configures the HTTP client used for requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) error {
		if httpClient == nil {
			return errors.New("memory: HTTP client cannot be nil")
		}
		c.httpClient = httpClient
		return nil
	}
}

// WithUserAgent changes the default User-Agent header.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) error {
		c.userAgent = userAgent
		return nil
	}
}

// NewClient constructs an authenticated client. baseURL may include a path
// prefix, for example https://example.com/memory.
func NewClient(baseURL, apiKey string, options ...Option) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("memory: parse base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("memory: base URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("memory: base URL must include a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("memory: base URL cannot contain credentials, a query, or a fragment")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("memory: API key cannot be empty")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	c := &Client{
		baseURL:    strings.TrimRight(parsed.String(), "/"),
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
		userAgent:  defaultUserAgent,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("memory: client option cannot be nil")
		}
		if err := option(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

type requestIDContextKey struct{}

// WithRequestID returns a context that sends requestID in X-Request-ID.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// ProblemCode identifies a stable API error condition.
type ProblemCode string

const (
	ProblemCodeMalformedRequest    ProblemCode = "malformed_request"
	ProblemCodeUnauthenticated     ProblemCode = "unauthenticated"
	ProblemCodeInsufficientScope   ProblemCode = "insufficient_scope"
	ProblemCodeSpaceNotFound       ProblemCode = "space_not_found"
	ProblemCodeMemoryNotFound      ProblemCode = "memory_not_found"
	ProblemCodeNotFound            ProblemCode = "not_found"
	ProblemCodeMethodNotAllowed    ProblemCode = "method_not_allowed"
	ProblemCodeIdempotencyConflict ProblemCode = "idempotency_conflict"
	ProblemCodeLinkConflict        ProblemCode = "link_conflict"
	ProblemCodePayloadTooLarge     ProblemCode = "payload_too_large"
	ProblemCodeValidationFailed    ProblemCode = "validation_failed"
	ProblemCodeRateLimited         ProblemCode = "rate_limited"
	ProblemCodeInternal            ProblemCode = "internal"
	ProblemCodeUnavailable         ProblemCode = "unavailable"
)

// FieldError describes one invalid request field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problem is the RFC 9457 error document returned by the API.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Code      ProblemCode  `json:"code"`
	Detail    string       `json:"detail,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// APIError is returned for non-2xx HTTP responses.
type APIError struct {
	StatusCode int
	Problem    Problem
	RequestID  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := e.Problem.Detail
	if message == "" {
		message = e.Problem.Title
	}
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	if e.Problem.Code != "" {
		return fmt.Sprintf("memory: API returned %d %s: %s", e.StatusCode, e.Problem.Code, message)
	}
	return fmt.Sprintf("memory: API returned %d: %s", e.StatusCode, message)
}

// IsProblem reports whether err is an APIError with the supplied stable code.
func IsProblem(err error, code ProblemCode) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Problem.Code == code
}

func (c *Client) do(
	ctx context.Context,
	method string,
	endpoint string,
	query url.Values,
	contentType string,
	body any,
	destination any,
) (int, error) {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			err = fmt.Errorf("memory: encode request: %w", err)
			recordSpanError(ctx, err)
			return 0, err
		}
		requestBody = bytes.NewReader(encoded)
	}

	requestURL := c.baseURL + endpoint
	if len(query) != 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, requestBody)
	if err != nil {
		err = fmt.Errorf("memory: create request: %w", err)
		recordSpanError(ctx, err)
		return 0, err
	}
	req.Header.Set("Accept", "application/json, application/problem+json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if requestID, ok := ctx.Value(requestIDContextKey{}).(string); ok && requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	injectTraceContext(ctx, req.Header)

	response, err := c.httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("memory: execute request: %w", err)
		recordSpanError(ctx, err)
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		err := decodeAPIError(response)
		recordSpanError(ctx, err)
		return response.StatusCode, err
	}
	if destination == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode, nil
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		err = fmt.Errorf("memory: decode response: %w", err)
		recordSpanError(ctx, err)
		return response.StatusCode, err
	}
	return response.StatusCode, nil
}

func decodeAPIError(response *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	problem := Problem{}
	if err := json.Unmarshal(body, &problem); err != nil {
		problem.Title = http.StatusText(response.StatusCode)
		problem.Detail = strings.TrimSpace(string(body))
		if problem.Detail == "" && readErr != nil {
			problem.Detail = readErr.Error()
		}
	}
	if problem.Status == 0 {
		problem.Status = response.StatusCode
	}
	requestID := problem.RequestID
	if requestID == "" {
		requestID = response.Header.Get("X-Request-ID")
	}
	return &APIError{
		StatusCode: response.StatusCode,
		Problem:    problem,
		RequestID:  requestID,
		RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}

func pathSegment(value string) string {
	return url.PathEscape(value)
}

func setInt(query url.Values, key string, value int) {
	if value != 0 {
		query.Set(key, strconv.Itoa(value))
	}
}

func setTime(query url.Values, key string, value *time.Time) {
	if value != nil {
		query.Set(key, value.Format(time.RFC3339Nano))
	}
}

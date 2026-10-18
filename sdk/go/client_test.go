package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *responseRecorder) Header() http.Header { return r.header }

func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(body)
}

func (r *responseRecorder) WriteHeader(status int) { r.status = status }

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := &responseRecorder{header: make(http.Header)}
		handler.ServeHTTP(recorder, request)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		return &http.Response{
			StatusCode: recorder.status,
			Header:     recorder.header,
			Body:       io.NopCloser(bytes.NewReader(recorder.body.Bytes())),
			Request:    request,
		}, nil
	})}
	client, err := NewClient("https://example.test/gateway", "emk_test_secret", WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRetainMemoryBuildsAuthenticatedRequest(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.URL.RequestURI(); got != "/gateway/v1/spaces/user%2Fa%20b/memories" {
			t.Errorf("request URI = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer emk_test_secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Request-ID"); got != "req-123" {
			t.Errorf("X-Request-ID = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var input MemoryInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if input.Content != "O usuário prefere respostas curtas" {
			t.Errorf("content = %q", input.Content)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true,"memory":{"id":"memory-1","content":"O usuário prefere respostas curtas"}}`))
	})

	ctx := WithRequestID(context.Background(), "req-123")
	result, err := client.RetainMemory(ctx, "user/a b", MemoryInput{
		Content: "O usuário prefere respostas curtas",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Memory.ID != "memory-1" {
		t.Fatalf("result = %+v", result)
	}
}

func TestListMemoriesEncodesFilters(t *testing.T) {
	after := time.Date(2026, 10, 18, 12, 30, 45, 123, time.FixedZone("BRT", -3*60*60))
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got, want := query["memory_type"], []string{"fact", "decision"}; !reflect.DeepEqual(got, want) {
			t.Errorf("memory_type = %v, want %v", got, want)
		}
		if got := query.Get("occurred_after"); got != after.Format(time.RFC3339Nano) {
			t.Errorf("occurred_after = %q", got)
		}
		if query.Get("limit") != "25" || query.Get("cursor") != "next/page" {
			t.Errorf("query = %v", query)
		}
		_, _ = w.Write([]byte(`{"items":[],"next_cursor":null}`))
	})

	page, err := client.ListMemories(context.Background(), "space", ListMemoriesOptions{
		Limit:         25,
		Cursor:        "next/page",
		MemoryTypes:   []MemoryType{MemoryTypeFact, MemoryTypeDecision},
		OccurredAfter: &after,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("items = %v", page.Items)
	}
}

func TestMemoryPatchEncodesNullAndZero(t *testing.T) {
	patch := MemoryPatch{
		Content:        Ptr("conteúdo novo"),
		ClearSummary:   true,
		ClearEmbedding: true,
		Importance:     Ptr(float32(0)),
		ClearExpiresAt: true,
		Metadata:       Metadata{"nested": map[string]any{"remove": nil}},
	}
	encoded, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"summary", "embedding", "expires_at"} {
		value, exists := got[field]
		if !exists || value != nil {
			t.Errorf("%s = %#v, exists %v", field, value, exists)
		}
	}
	if value, exists := got["importance"]; !exists || value != float64(0) {
		t.Errorf("importance = %#v, exists %v", value, exists)
	}
	if _, exists := got["clear_summary"]; exists {
		t.Error("control field was encoded")
	}
}

func TestMemoryPatchRejectsConflictingInstructions(t *testing.T) {
	_, err := json.Marshal(MemoryPatch{Summary: Ptr("keep"), ClearSummary: true})
	if err == nil || !strings.Contains(err.Error(), "Summary and ClearSummary") {
		t.Fatalf("error = %v", err)
	}
}

func TestAPIErrorIsTyped(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Retry-After", "3")
		w.Header().Set("X-Request-ID", "header-id")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{
			"type":"https://example.test/problems/rate-limited",
			"title":"Muitas requisições",
			"status":429,
			"code":"rate_limited",
			"detail":"tente novamente",
			"request_id":"body-id"
		}`))
	})

	_, err := client.Recall(context.Background(), "space", RecallRequest{Query: "preferências"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsProblem(err, ProblemCodeRateLimited) {
		t.Fatalf("IsProblem returned false for %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != 429 || apiErr.RequestID != "body-id" || apiErr.RetryAfter != 3*time.Second {
		t.Fatalf("API error = %+v", apiErr)
	}
}

func TestUnexpectedErrorResponsePreservesDetail(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "request-id")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	})

	err := client.Readiness(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v", err)
	}
	if apiErr.Problem.Detail != "upstream unavailable" || apiErr.RequestID != "request-id" {
		t.Fatalf("API error = %+v", apiErr)
	}
}

func TestUpsertSpaceReportsCreationAndMergePatchContentType(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"space-1","external_id":"user-1","name":"User"}`))
		case 2:
			if got := r.Header.Get("Content-Type"); got != "application/merge-patch+json" {
				t.Errorf("Content-Type = %q", got)
			}
			_, _ = w.Write([]byte(`{"id":"memory-1","content":"updated"}`))
		}
	})

	_, created, err := client.UpsertSpace(context.Background(), "user-1", SpaceInput{Name: "User"})
	if err != nil || !created {
		t.Fatalf("created = %v, error = %v", created, err)
	}
	_, err = client.UpdateMemory(context.Background(), "user-1", "memory-1", MemoryPatch{Content: Ptr("updated")})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNewClientValidatesConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, baseURL, apiKey string
	}{
		{"relative URL", "/memory", "key"},
		{"unsupported scheme", "ftp://example.com", "key"},
		{"URL credentials", "https://user@example.com", "key"},
		{"empty API key", "https://example.com", " "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewClient(test.baseURL, test.apiKey); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if _, err := NewClient("https://example.com", "key", WithHTTPClient(nil)); err == nil {
		t.Fatal("expected nil HTTP client error")
	}
}

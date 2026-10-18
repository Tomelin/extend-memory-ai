package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/middleware"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

type fakeAuth struct {
	mu       sync.Mutex
	tokens   map[string]domain.Principal
	err      error
	failures []string
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (domain.Principal, error) {
	if f.err != nil {
		return domain.Principal{}, f.err
	}
	p, ok := f.tokens[token]
	if !ok {
		return domain.Principal{}, auth.ErrUnauthenticated
	}
	return p, nil
}

func (f *fakeAuth) RecordFailure(_ context.Context, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, reason)
}

var principal = domain.Principal{ProjectID: uuid.New(), KeyID: uuid.New(), Scopes: []domain.Scope{domain.ScopeMemoriesRead}}

func newEngine(handlers ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.Any("/x", handlers...)
	return r
}

func do(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) api.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Fatalf("Content-Type = %q", ct)
	}
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func ok(c *gin.Context) { c.Status(http.StatusNoContent) }

func TestAuthenticate(t *testing.T) {
	fa := &fakeAuth{tokens: map[string]domain.Principal{"good": principal}}
	var seen domain.Principal
	r := newEngine(middleware.Authenticate(fa), func(c *gin.Context) {
		seen, _ = auth.PrincipalFromContext(c.Request.Context())
		c.Status(http.StatusNoContent)
	})

	cases := []struct {
		name, header string
		status       int
	}{
		{"missing", "", 401},
		{"wrong scheme", "Basic good", 401},
		{"empty token", "Bearer ", 401},
		{"unknown token", "Bearer bad", 401},
		{"valid", "Bearer good", 204},
		{"scheme is case-insensitive", "bearer good", 204},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := do(r, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.status == 401 {
				p := decodeProblem(t, rec)
				if p.Code != api.ProblemCodeUnauthenticated || p.RequestId == nil || *p.RequestId != rec.Header().Get(middleware.HeaderRequestID) {
					t.Errorf("unexpected problem %+v", p)
				}
			} else if seen.ProjectID != principal.ProjectID {
				t.Errorf("principal not propagated: %+v", seen)
			}
		})
	}
	if len(fa.failures) < 3 || fa.failures[0] != auth.ReasonMissing || fa.failures[1] != auth.ReasonInvalid {
		t.Errorf("failures recorded = %v", fa.failures)
	}
}

func TestAuthenticateStoreUnavailable(t *testing.T) {
	fa := &fakeAuth{err: domain.ErrUnavailable}
	r := newEngine(middleware.Authenticate(fa), ok)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer any")
	rec := do(r, req)
	if rec.Code != http.StatusServiceUnavailable || decodeProblem(t, rec).Code != api.ProblemCodeUnavailable {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
}

func TestRequireScope(t *testing.T) {
	fa := &fakeAuth{tokens: map[string]domain.Principal{"good": principal}}
	for scope, want := range map[domain.Scope]int{domain.ScopeMemoriesRead: 204, domain.ScopeMemoriesWrite: 403} {
		r := newEngine(middleware.Authenticate(fa), middleware.RequireScope(fa, scope), ok)
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := do(r, req)
		if rec.Code != want {
			t.Errorf("scope %s: status = %d, want %d", scope, rec.Code, want)
		}
		if want == 403 && decodeProblem(t, rec).Code != api.ProblemCodeInsufficientScope {
			t.Errorf("scope %s: wrong code", scope)
		}
	}
}

func TestRequestID(t *testing.T) {
	r := newEngine(ok)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.HeaderRequestID, "client-id-1")
	if got := do(r, req).Header().Get(middleware.HeaderRequestID); got != "client-id-1" {
		t.Errorf("valid id not echoed: %q", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.HeaderRequestID, "bad id\nwith newline")
	got := do(r, req).Header().Get(middleware.HeaderRequestID)
	if _, err := uuid.Parse(got); err != nil {
		t.Errorf("invalid id not replaced by a generated one: %q", got)
	}
}

func TestBodyLimit(t *testing.T) {
	r := newEngine(middleware.BodyLimit(8), func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusNoContent)
	})
	rec := do(r, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("0123456789")))
	if rec.Code != http.StatusRequestEntityTooLarge || decodeProblem(t, rec).Code != api.ProblemCodePayloadTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec := do(r, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("0123"))); rec.Code != http.StatusNoContent {
		t.Fatalf("small body: status = %d", rec.Code)
	}
}

func TestRecover(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := newEngine(middleware.Recover(logger), func(*gin.Context) { panic("boom") })
	rec := do(r, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError || decodeProblem(t, rec).Code != api.ProblemCodeInternal {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("panic value leaked to the client")
	}
}

func TestLoggerOmitsCredentials(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	fa := &fakeAuth{tokens: map[string]domain.Principal{"emk_secret": principal}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Logger(logger))
	r.GET("/v1/items/:id", middleware.Authenticate(fa), ok)

	req := httptest.NewRequest(http.MethodGet, "/v1/items/external-id-123", nil)
	req.Header.Set("Authorization", "Bearer emk_secret")
	req.Header.Set(middleware.HeaderRequestID, "req-42")
	do(r, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	if line["request_id"] != "req-42" || line["http.route"] != "/v1/items/:id" ||
		line["project_id"] != principal.ProjectID.String() || line["key_id"] != principal.KeyID.String() {
		t.Errorf("unexpected log line %v", line)
	}
	for _, leaked := range []string{"emk_secret", "external-id-123"} {
		if strings.Contains(buf.String(), leaked) {
			t.Errorf("log contains %q", leaked)
		}
	}
}

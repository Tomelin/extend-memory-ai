package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/middleware"
)

func TestRateLimitPerKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limit, err := middleware.RateLimit(noop.NewMeterProvider().Meter("t"), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	keyA, keyB := uuid.New(), uuid.New()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		key := keyA
		if c.GetHeader("X-Key") == "b" {
			key = keyB
		}
		ctx := auth.WithPrincipal(c.Request.Context(), domain.Principal{KeyID: key})
		c.Request = c.Request.WithContext(ctx)
	}, limit)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	do := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		req.Header.Set("X-Key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for i := range 2 {
		if w := do("a"); w.Code != http.StatusNoContent {
			t.Fatalf("request %d: status %d", i, w.Code)
		}
	}
	w := do("a")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("status %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := do("b"); w.Code != http.StatusNoContent {
		t.Fatalf("other key limited: %d", w.Code)
	}
}

func TestRateLimitDisabled(t *testing.T) {
	limit, err := middleware.RateLimit(noop.NewMeterProvider().Meter("t"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(limit)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for range 10 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("status %d", w.Code)
		}
	}
}

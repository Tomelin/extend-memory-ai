package problem

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
)

func run(t *testing.T, h gin.HandlerFunc) (*httptest.ResponseRecorder, api.Problem) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set(RequestIDKey, "req-1")
	h(c)
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, ContentType)
	}
	return rec, p
}

func TestAbort(t *testing.T) {
	rec, p := run(t, func(c *gin.Context) {
		Abort(c, http.StatusUnauthorized, api.ProblemCodeUnauthenticated, "sem chave")
		if !c.IsAborted() || c.GetString(ErrorCodeKey) != "unauthenticated" {
			t.Error("context not aborted or error code not stored")
		}
	})
	if rec.Code != http.StatusUnauthorized || p.Status != 401 || p.Code != api.ProblemCodeUnauthenticated ||
		p.Type != "https://extend-memory-ai/errors/unauthenticated" || p.Title == "" ||
		p.Detail == nil || *p.Detail != "sem chave" || p.RequestId == nil || *p.RequestId != "req-1" {
		t.Errorf("unexpected problem %d %+v", rec.Code, p)
	}
}

func TestAbortWithError(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   api.ProblemCode
	}{
		{domain.ErrNotFound, 404, api.ProblemCodeNotFound},
		{domain.ErrValidation, 422, api.ProblemCodeValidationFailed},
		{domain.ErrUnavailable, 503, api.ProblemCodeUnavailable},
		{domain.ErrRetryable, 503, api.ProblemCodeUnavailable},
		{errors.New("secret detail"), 500, api.ProblemCodeInternal},
	}
	for _, tc := range cases {
		rec, p := run(t, func(c *gin.Context) { AbortWithError(c, tc.err) })
		if rec.Code != tc.status || p.Code != tc.code || p.Detail != nil {
			t.Errorf("%v: got %d %+v", tc.err, rec.Code, p)
		}
	}
}

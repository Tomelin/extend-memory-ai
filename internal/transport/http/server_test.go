package httptransport_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	httptransport "github.com/Tomelin/extend-memory-ai/internal/transport/http"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

type projects map[uuid.UUID]domain.Project

func (p projects) GetProject(_ context.Context, id uuid.UUID) (domain.Project, error) {
	if pr, ok := p[id]; ok {
		return pr, nil
	}
	return domain.Project{}, domain.ErrNotFound
}

type authn map[string]domain.Principal

func (a authn) Authenticate(_ context.Context, token string) (domain.Principal, error) {
	if p, ok := a[token]; ok {
		return p, nil
	}
	return domain.Principal{}, auth.ErrUnauthenticated
}

func (authn) RecordFailure(context.Context, string) {}

func newRouter(t *testing.T, ping error) (http.Handler, domain.Project) {
	t.Helper()
	project := domain.Project{ID: uuid.New(), Slug: "assistente", Name: "Assistente"}
	r, err := httptransport.NewRouter(httptransport.Deps{
		ServiceName:   "memory-api-test",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:        pinger{err: ping},
		Projects:      projects{project.ID: project},
		Authenticator: authn{"good": {ProjectID: project.ID, KeyID: uuid.New()}},
		MaxBodyBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, project
}

func request(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	h, project := newRouter(t, nil)

	if rec := request(h, http.MethodGet, "/healthz", ""); rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}
	if rec := request(h, http.MethodGet, "/readyz", ""); rec.Code != 200 {
		t.Errorf("readyz = %d", rec.Code)
	}

	rec := request(h, http.MethodGet, "/v1/project", "good")
	if rec.Code != 200 {
		t.Fatalf("project = %d %s", rec.Code, rec.Body)
	}
	var got api.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Id != project.ID || got.Slug != project.Slug || got.Name != project.Name {
		t.Errorf("project = %+v", got)
	}

	for _, tc := range []struct {
		method, path, token string
		status              int
		code                api.ProblemCode
	}{
		{http.MethodGet, "/v1/project", "", 401, api.ProblemCodeUnauthenticated},
		{http.MethodGet, "/v1/rota-inexistente", "", 404, api.ProblemCodeNotFound},
		{http.MethodPost, "/healthz", "", 405, api.ProblemCodeMethodNotAllowed},
	} {
		rec := request(h, tc.method, tc.path, tc.token)
		var p api.Problem
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		if rec.Code != tc.status || p.Code != tc.code || rec.Header().Get("Content-Type") != problem.ContentType {
			t.Errorf("%s %s = %d %s (%s)", tc.method, tc.path, rec.Code, p.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestReadinessFailsWhenDatabaseIsDown(t *testing.T) {
	h, _ := newRouter(t, errors.New("connection refused"))
	if rec := request(h, http.MethodGet, "/healthz", ""); rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}
	rec := request(h, http.MethodGet, "/readyz", "")
	var p api.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 503 || p.Code != api.ProblemCodeUnavailable {
		t.Errorf("readyz = %d %s", rec.Code, p.Code)
	}
}

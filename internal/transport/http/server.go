// Package httptransport exposes the public HTTP API with gin.
package httptransport

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/middleware"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

const instrumentationName = "github.com/Tomelin/extend-memory-ai/internal/transport/http"

// HealthChecker reports whether dependencies are reachable.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// ProjectReader reads the caller's project.
type ProjectReader interface {
	GetProject(ctx context.Context, projectID uuid.UUID) (domain.Project, error)
}

// Deps are the router dependencies.
type Deps struct {
	ServiceName   string
	Logger        *slog.Logger
	Health        HealthChecker
	Projects      ProjectReader
	Memories      MemoryService
	Authenticator middleware.Authenticator
	MaxBodyBytes  int64
	// RateLimitRPS <= 0 disables the per-key rate limit.
	RateLimitRPS   int
	RateLimitBurst int
}

// NewRouter builds the gin engine with the middleware chain and routes.
func NewRouter(d Deps) (*gin.Engine, error) {
	active, err := middleware.ActiveRequests(otel.Meter(instrumentationName))
	if err != nil {
		return nil, err
	}

	r := gin.New()
	r.UseRawPath = true
	r.UnescapePathValues = true
	r.HandleMethodNotAllowed = true

	r.Use(
		otelgin.Middleware(d.ServiceName, otelgin.WithGinMetricAttributeFn(errorCodeAttribute)),
		middleware.RequestID(),
		middleware.Logger(d.Logger),
		middleware.Recover(d.Logger),
		active,
		middleware.BodyLimit(d.MaxBodyBytes),
	)
	r.NoRoute(func(c *gin.Context) {
		problem.Abort(c, http.StatusNotFound, api.ProblemCodeNotFound, "rota inexistente")
	})
	r.NoMethod(func(c *gin.Context) {
		problem.Abort(c, http.StatusMethodNotAllowed, api.ProblemCodeMethodNotAllowed, "")
	})

	h := handlers{deps: d}
	r.GET("/healthz", h.liveness)
	r.GET("/readyz", h.readiness)

	limit, err := middleware.RateLimit(otel.Meter(instrumentationName), float64(d.RateLimitRPS), d.RateLimitBurst)
	if err != nil {
		return nil, err
	}
	v1 := r.Group("/v1", middleware.Authenticate(d.Authenticator), limit)
	v1.GET("/project", h.getCurrentProject)

	read := middleware.RequireScope(d.Authenticator, domain.ScopeMemoriesRead)
	write := middleware.RequireScope(d.Authenticator, domain.ScopeMemoriesWrite)
	spacesWrite := middleware.RequireScope(d.Authenticator, domain.ScopeSpacesWrite)
	v1.GET("/spaces", read, h.listSpaces)
	spaces := v1.Group("/spaces/:space")
	spaces.PUT("", spacesWrite, h.upsertSpace)
	spaces.GET("", read, h.getSpace)
	spaces.DELETE("", spacesWrite, h.deleteSpace)
	spaces.POST("/memories", write, h.retainMemory)
	spaces.GET("/memories", read, h.listMemories)
	spaces.POST("/memories/batch", write, h.retainMemoriesBatch)
	spaces.GET("/memories/:memory_id", read, h.getMemory)
	spaces.DELETE("/memories/:memory_id", write, h.deleteMemory)
	spaces.PATCH("/memories/:memory_id", write, h.updateMemory)
	spaces.POST("/recall", read, h.recall)
	spaces.POST("/memories/:memory_id/links", write, h.createLink)
	spaces.GET("/memories/:memory_id/links", read, h.listLinks)
	spaces.DELETE("/memories/:memory_id/links/:target_id/:relation_type", write, h.deleteLink)

	return r, nil
}

func errorCodeAttribute(c *gin.Context) []attribute.KeyValue {
	if code := c.GetString(problem.ErrorCodeKey); code != "" {
		return []attribute.KeyValue{attribute.String("error.code", code)}
	}
	return nil
}

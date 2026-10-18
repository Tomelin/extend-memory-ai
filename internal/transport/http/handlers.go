package httptransport

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

const readinessTimeout = 2 * time.Second

type handlers struct {
	deps Deps
}

func (h handlers) liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h handlers) readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
	defer cancel()
	if err := h.deps.Health.Ping(ctx); err != nil {
		problem.Abort(c, http.StatusServiceUnavailable, api.ProblemCodeUnavailable, "banco de dados indisponível")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (h handlers) getCurrentProject(c *gin.Context) {
	ctx := c.Request.Context()
	p, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		problem.Abort(c, http.StatusUnauthorized, api.ProblemCodeUnauthenticated, "")
		return
	}
	project, err := h.deps.Projects.GetProject(ctx, p.ProjectID)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, api.Project{
		Id:          project.ID,
		Slug:        project.Slug,
		Name:        project.Name,
		Description: project.Description,
	})
}

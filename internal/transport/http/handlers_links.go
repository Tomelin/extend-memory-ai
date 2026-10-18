package httptransport

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

func (h handlers) createLink(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	source, ok := memoryID(c)
	if !ok {
		return
	}
	var body api.LinkInput
	if !decodeJSON(c, &body) {
		return
	}
	l, err := h.deps.Memories.CreateLink(c.Request.Context(), p.ProjectID, c.Param("space"), domain.LinkInput{
		SourceMemoryID: source,
		TargetMemoryID: body.TargetMemoryId,
		RelationType:   domain.RelationType(body.RelationType),
		Weight:         body.Weight,
		Metadata:       fromAPIMetadata(body.Metadata),
	})
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toAPILink(l))
}

func (h handlers) listLinks(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := memoryID(c)
	if !ok {
		return
	}
	f := domain.LinkFilter{
		Direction:    domain.LinkDirection(c.Query("direction")),
		RelationType: domain.RelationType(c.Query("relation_type")),
	}
	links, err := h.deps.Memories.ListLinks(c.Request.Context(), p.ProjectID, c.Param("space"), id, f)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toAPILinks(links)})
}

func (h handlers) deleteLink(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	source, ok := memoryID(c)
	if !ok {
		return
	}
	target, ok := uuidParam(c, "target_id")
	if !ok {
		return
	}
	err := h.deps.Memories.DeleteLink(c.Request.Context(), p.ProjectID, c.Param("space"), source, target,
		domain.RelationType(c.Param("relation_type")))
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

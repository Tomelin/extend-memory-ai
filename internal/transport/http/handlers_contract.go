package httptransport

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

func (h handlers) listSpaces(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	in := domain.ListSpacesInput{Kind: domain.SpaceKind(c.Query("kind"))}
	if raw, ok := c.GetQuery("limit"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil {
			problem.Abort(c, http.StatusBadRequest, api.ProblemCodeMalformedRequest, "limit deve ser um número inteiro")
			return
		}
		in.Limit = &n
	}
	if raw, ok := c.GetQuery("cursor"); ok {
		after, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(after) == 0 {
			problem.Abort(c, http.StatusBadRequest, api.ProblemCodeMalformedRequest, errInvalidCursor.Error())
			return
		}
		s := string(after)
		in.After = &s
	}
	page, err := h.deps.Memories.ListSpaces(c.Request.Context(), p.ProjectID, in)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	resp := api.SpacePage{Items: make([]api.Space, len(page.Items))}
	for i, sp := range page.Items {
		resp.Items[i] = toAPISpace(sp)
	}
	if page.Next != nil {
		next := base64.RawURLEncoding.EncodeToString([]byte(*page.Next))
		resp.NextCursor = &next
	}
	c.JSON(http.StatusOK, resp)
}

func (h handlers) updateMemory(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := memoryID(c)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if !decodeJSON(c, &raw) {
		return
	}
	patch, err := parseMemoryPatch(raw)
	if err != nil {
		problem.Abort(c, http.StatusBadRequest, api.ProblemCodeMalformedRequest, err.Error())
		return
	}
	m, err := h.deps.Memories.UpdateMemory(c.Request.Context(), p.ProjectID, c.Param("space"), id, patch)
	if err != nil {
		problem.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAPIMemory(m))
}

// parseMemoryPatch keeps the distinction between absent and null members,
// which a struct decode would lose.
func parseMemoryPatch(raw map[string]json.RawMessage) (domain.MemoryPatch, error) {
	var p domain.MemoryPatch
	if len(raw) == 0 {
		return p, errors.New("informe ao menos um campo")
	}
	for key, value := range raw {
		var err error
		switch key {
		case "content":
			err = patchField(value, &p.Content)
		case "summary":
			err = patchField(value, &p.Summary)
		case "importance":
			err = patchField(value, &p.Importance)
		case "expires_at":
			err = patchField(value, &p.ExpiresAt)
		case "metadata":
			err = patchField(value, &p.Metadata)
		case "embedding":
			var e domain.PatchField[api.Embedding]
			if err = patchField(value, &e); err == nil {
				p.Embedding = domain.PatchField[domain.Embedding]{
					Set: e.Set, Null: e.Null, Value: domain.Embedding{Model: e.Value.Model, Vector: e.Value.Vector},
				}
			}
		default:
			if !slices.Contains(domain.ImmutableMemoryFields, key) {
				return p, errors.New("campo desconhecido: " + key)
			}
			p.Immutable = append(p.Immutable, key)
		}
		if err != nil {
			return p, errors.New(key + ": " + err.Error())
		}
	}
	slices.Sort(p.Immutable)
	return p, nil
}

func patchField[T any](raw json.RawMessage, f *domain.PatchField[T]) error {
	f.Set = true
	if string(raw) == "null" {
		f.Null = true
		return nil
	}
	return strictUnmarshal(raw, &f.Value)
}

func strictUnmarshal(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
)

// apiScenario runs features/api_memoria.feature through the HTTP API.
type apiScenario struct {
	*scenario
	spaces   map[string]uuid.UUID // project slug -> usuario-123 space id
	keys     map[string]string    // project slug -> token
	memories map[string]uuid.UUID // label -> memory id

	lastPath    string
	lastBody    map[string]any
	firstID     string
	memoryID    string
	batchMarker string
	nextCursor  string
}

const defaultSpace = "usuario-123"

func initAPIScenario(sc *godog.ScenarioContext, e *env) {
	s := &apiScenario{
		scenario: &scenario{env: e, target: e.app.Handler, projects: map[string]uuid.UUID{}},
		spaces:   map[string]uuid.UUID{},
		keys:     map[string]string{},
		memories: map[string]uuid.UUID{},
	}

	// Context.
	sc.Step(`^que existem os projetos "([^"]+)" e "([^"]+)"$`, s.createProjects)
	sc.Step(`^que cada projeto possui o espaço "([^"]+)"$`, s.createSpaces)
	sc.Step(`^que o projeto "([^"]+)" possui uma chave com escopos "([^"]+)"$`, s.issueProjectKey)

	// Credentials.
	sc.Step(`^envio "(GET|DELETE) ([^"]+)" sem o cabeçalho Authorization$`, func(method, path string) {
		s.send(method, path, "", nil)
	})
	sc.Step(`^envio "(GET|DELETE) ([^"]+)"(?: com essa chave)?$`, func(method, path string) {
		s.send(method, path, s.token, nil)
	})
	sc.Step(`^que a chave do projeto "([^"]+)" foi revogada$`, func(slug string) error {
		prefix, _, err := auth.ParseToken(s.keys[slug])
		if err != nil {
			return err
		}
		return s.cli("key", "revoke", "--prefix", prefix)
	})
	sc.Step(`^que o cache de chaves expirou$`, func() { time.Sleep(keyCacheTTL + 200*time.Millisecond) })
	sc.Step(`^que estou autenticado com uma chave que possui apenas "([^"]+)"$`, func(scope string) error {
		return s.issueKey("assistente-financeiro", scope)
	})
	sc.Step(`^que estou autenticado no projeto "([^"]+)"$`, func(slug string) error {
		if k, ok := s.keys[slug]; ok {
			s.token = k
			return nil
		}
		return s.issueProjectKey(slug, "memories:read memories:write")
	})
	sc.Step(`^que existe a memória "([^"]+)" no projeto "([^"]+)"$`, s.insertMemoryDirectly)
	sc.Step(`^consulto a memória "([^"]+)" no espaço "([^"]+)"$`, func(label, space string) {
		s.send(http.MethodGet, "/v1/spaces/"+space+"/memories/"+s.memories[label].String(), s.token, nil)
	})

	// Responses.
	sc.Step(`^a resposta deve ter status (\d+)$`, s.statusIs)
	sc.Step(`^a resposta deve ter status (\d+) e código "([^"]+)"$`, s.statusAndCode)
	sc.Step(`^o corpo deve conter "([^"]+)" igual a (verdadeiro|falso)$`, func(field, value string) error {
		return s.fieldEquals(field, value == "verdadeiro")
	})
	sc.Step(`^o corpo deve conter "([^"]+)" igual a "([^"]+)"$`, func(field, value string) error {
		return s.fieldEquals(field, value)
	})
	sc.Step(`^o Content-Type deve ser "([^"]+)"$`, func(ct string) error {
		if got := s.resp.Header().Get("Content-Type"); got != ct {
			return fmt.Errorf("Content-Type = %q", got)
		}
		return nil
	})
	sc.Step(`^o corpo deve conter "([^"]+)", "([^"]+)" e "([^"]+)"$`, func(a, b, c string) error {
		body, err := s.body()
		if err != nil {
			return err
		}
		for _, k := range []string{a, b, c} {
			if _, ok := body[k]; !ok {
				return fmt.Errorf("body %s has no %q", s.resp.Body, k)
			}
		}
		return nil
	})
	sc.Step(`^qualquer requisição resulta em erro$`, func() {
		s.send(http.MethodGet, "/v1/spaces/espaco-inexistente", s.token, nil)
	})

	// Retain.
	sc.Step(`^envio "POST ([^"]+)" com:$`, func(path string, table *godog.Table) error {
		if len(table.Rows) != 2 {
			return errors.New("expected a header and one data row")
		}
		body := map[string]any{}
		for i, cell := range table.Rows[0].Cells {
			value := table.Rows[1].Cells[i].Value
			if group, key, ok := strings.Cut(cell.Value, "."); ok {
				sub, _ := body[group].(map[string]any)
				if sub == nil {
					sub = map[string]any{}
					body[group] = sub
				}
				sub[key] = value
				continue
			}
			body[cell.Value] = value
		}
		s.send(http.MethodPost, path, s.token, body)
		return nil
	})
	sc.Step(`^a memória retornada deve informar a origem "([^"]+)" e a referência "([^"]+)"$`, func(typ, ref string) error {
		var r struct {
			Memory struct{ Source struct{ Type, Ref string } }
		}
		if err := json.Unmarshal(s.resp.Body.Bytes(), &r); err != nil {
			return err
		}
		if r.Memory.Source.Type != typ || r.Memory.Source.Ref != ref {
			return fmt.Errorf("source = %+v", r.Memory.Source)
		}
		return nil
	})
	sc.Step(`^gravo uma memória no espaço "([^"]+)"$`, func(space string) {
		s.send(http.MethodPost, "/v1/spaces/"+space+"/memories", s.token, map[string]any{"content": "Memória de teste"})
	})
	sc.Step(`^que gravei a memória "([^"]+)" com a chave de idempotência "([^"]+)"$`, func(content, key string) error {
		s.retain(map[string]any{"content": content, "idempotency_key": key})
		if err := s.statusIs(http.StatusCreated); err != nil {
			return err
		}
		s.firstID = s.memoryID
		return nil
	})
	sc.Step(`^repito exatamente a mesma requisição$`, func() { s.send(http.MethodPost, s.lastPath, s.token, s.lastBody) })
	sc.Step(`^o identificador deve ser o mesmo da primeira gravação$`, func() error {
		if s.memoryID != s.firstID {
			return fmt.Errorf("id = %s, want %s", s.memoryID, s.firstID)
		}
		return nil
	})
	sc.Step(`^gravo a memória "([^"]+)" com a mesma chave$`, func(content string) {
		s.retain(map[string]any{"content": content, "idempotency_key": s.lastBody["idempotency_key"]})
	})
	sc.Step(`^gravo uma memória com um embedding de (\d+) dimensões$`, func(dims int) {
		s.retain(map[string]any{"content": "Memória com embedding", "embedding": embedding(dims, "modelo-a")})
	})
	sc.Step(`^o erro deve apontar o campo "([^"]+)"$`, func(field string) error {
		var p struct{ Errors []struct{ Field string } }
		if err := json.Unmarshal(s.resp.Body.Bytes(), &p); err != nil {
			return err
		}
		if !slices.ContainsFunc(p.Errors, func(e struct{ Field string }) bool { return e.Field == field }) {
			return fmt.Errorf("errors %s do not point to %s", s.resp.Body, field)
		}
		return nil
	})
	sc.Step(`^envio um lote com (\d+) memórias em que a terceira tem "importance" igual a (\d+)$`, func(n, importance int) {
		s.batchMarker = "lote-" + uuid.NewString()
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{"content": fmt.Sprintf("%s item %d", s.batchMarker, i)}
		}
		items[2]["importance"] = importance
		s.send(http.MethodPost, "/v1/spaces/"+defaultSpace+"/memories/batch", s.token, map[string]any{"items": items})
	})
	sc.Step(`^nenhuma das (\d+) memórias deve ter sido gravada$`, func(ctx context.Context, _ int) error {
		var n int
		if err := s.adminDB.QueryRow(ctx, "SELECT count(*) FROM memories WHERE content LIKE $1", s.batchMarker+"%").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("%d memories were stored", n)
		}
		return nil
	})

	// Spaces.
	sc.Step(`^envio "PUT ([^"]+)" com o nome "([^"]+)"$`, func(path, name string) {
		s.send(http.MethodPut, path, s.token, map[string]any{"name": name, "kind": "user"})
	})
	sc.Step(`^nenhuma memória do projeto "([^"]+)" deve existir no banco$`, func(ctx context.Context, slug string) error {
		var n int
		if err := s.adminDB.QueryRow(ctx, "SELECT count(*) FROM memories WHERE project_id = $1", s.projects[slug]).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("%d memories remain", n)
		}
		return nil
	})

	// Listing and deletion.
	sc.Step(`^que existem (\d+) memórias gravadas no espaço "([^"]+)"$`, func(n int, space string) error {
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{
				"content":     fmt.Sprintf("Memória %d", i),
				"occurred_at": time.Date(2026, 10, 1+i, 12, 0, 0, 0, time.UTC),
			}
		}
		s.send(http.MethodPost, "/v1/spaces/"+space+"/memories/batch", s.token, map[string]any{"items": items})
		return s.statusIs(http.StatusOK)
	})
	sc.Step(`^listo as memórias do espaço "([^"]+)" com "limit" igual a (\d+)$`, func(space string, limit int) {
		s.lastPath = fmt.Sprintf("/v1/spaces/%s/memories?limit=%d", space, limit)
		s.send(http.MethodGet, s.lastPath, s.token, nil)
	})
	sc.Step(`^listo a próxima página$`, func() { s.send(http.MethodGet, s.lastPath+"&cursor="+s.nextCursor, s.token, nil) })
	sc.Step(`^devem ser retornadas (\d+) memórias e um "next_cursor"$`, func(n int) error { return s.page(n, true) })
	sc.Step(`^deve ser retornada (\d+) memória e nenhum "next_cursor"$`, func(n int) error { return s.page(n, false) })
	sc.Step(`^excluo essa memória$`, func() {
		s.send(http.MethodDelete, "/v1/spaces/"+defaultSpace+"/memories/"+s.memoryID, s.token, nil)
	})
	sc.Step(`^consultar essa memória deve retornar status (\d+) e código "([^"]+)"$`, func(status int, code string) error {
		s.send(http.MethodGet, "/v1/spaces/"+defaultSpace+"/memories/"+s.memoryID, s.token, nil)
		return s.statusAndCode(status, code)
	})
	sc.Step(`^um recall por "([^"]+)" não deve retorná-la$`, func(query string) error {
		s.recall(map[string]any{"query": query})
		if err := s.statusIs(http.StatusOK); err != nil {
			return err
		}
		if s.recalled(s.memoryID) != nil {
			return fmt.Errorf("deleted memory was recalled: %s", s.resp.Body)
		}
		return nil
	})

	// Recall.
	sc.Step(`^que existe a memória "([^"]+)" com embedding do modelo "([^"]+)"$`, func(content, model string) error {
		s.retain(map[string]any{"content": content, "embedding": embedding(1536, model)})
		return s.statusIs(http.StatusCreated)
	})
	sc.Step(`^envio "POST ([^"]+)" com a consulta "([^"]+)" e um embedding do modelo "([^"]+)"$`, func(path, query, model string) {
		s.send(http.MethodPost, path, s.token, map[string]any{"query": query, "query_embedding": embedding(1536, model)})
	})
	sc.Step(`^a memória deve ser retornada$`, func() error {
		if s.recalled(s.memoryID) == nil {
			return fmt.Errorf("memory %s not in %s", s.memoryID, s.resp.Body)
		}
		return nil
	})
	sc.Step(`^"matched_by" deve conter "([^"]+)"$`, func(strategy string) error {
		item := s.recalled(s.memoryID)
		if item == nil || !slices.Contains(item.MatchedBy, strategy) {
			return fmt.Errorf("matched_by does not contain %s: %s", strategy, s.resp.Body)
		}
		return nil
	})
	sc.Step(`^que existe uma memória com embedding do modelo "([^"]+)" e sem termos em comum com a consulta$`, func(model string) error {
		s.retain(map[string]any{"content": "Planilha trimestral de despesas", "embedding": embedding(1536, model)})
		return s.statusIs(http.StatusCreated)
	})
	sc.Step(`^envio um recall somente com embedding do modelo "([^"]+)"$`, func(model string) {
		s.recall(map[string]any{"query_embedding": embedding(1536, model)})
	})
	sc.Step(`^a memória não deve ser retornada$`, func() error {
		if err := s.statusIs(http.StatusOK); err != nil {
			return err
		}
		if s.recalled(s.memoryID) != nil {
			return fmt.Errorf("memory of another model was recalled: %s", s.resp.Body)
		}
		return nil
	})

	// Links.
	sc.Step(`^que existem as memórias "([^"]+)" no espaço "([^"]+)" e "([^"]+)" no espaço "([^"]+)"$`, s.memoriesInSpaces)
	sc.Step(`^(?:que )?cri(?:o|ei) a relação "([^"]+)" de "([^"]+)" para "([^"]+)" no espaço "([^"]+)"$`, func(rel, from, to, space string) {
		s.send(http.MethodPost, "/v1/spaces/"+space+"/memories/"+s.memories[from].String()+"/links", s.token,
			map[string]any{"target_memory_id": s.memories[to], "relation_type": rel})
	})
	sc.Step(`^listo as relações de entrada de "([^"]+)" no espaço "([^"]+)"$`, func(label, space string) {
		s.send(http.MethodGet, "/v1/spaces/"+space+"/memories/"+s.memories[label].String()+"/links?direction=incoming", s.token, nil)
	})
	sc.Step(`^deve existir somente a relação "([^"]+)" de "([^"]+)" para "([^"]+)"$`, func(rel, from, to string) error {
		if err := s.statusIs(http.StatusOK); err != nil {
			return err
		}
		var r struct{ Items []link }
		if err := json.Unmarshal(s.resp.Body.Bytes(), &r); err != nil {
			return err
		}
		if len(r.Items) != 1 || !r.Items[0].is(rel, s.memories[from], s.memories[to]) {
			return fmt.Errorf("links = %s", s.resp.Body)
		}
		return nil
	})
	sc.Step(`^removo a relação "([^"]+)" de "([^"]+)" para "([^"]+)" no espaço "([^"]+)"$`, func(rel, from, to, space string) {
		s.send(http.MethodDelete, fmt.Sprintf("/v1/spaces/%s/memories/%s/links/%s/%s", space, s.memories[from], s.memories[to], rel), s.token, nil)
	})
	sc.Step(`^envio um recall por "([^"]+)" incluindo relações$`, func(query string) {
		s.recall(map[string]any{"query": query, "include_links": true})
	})
	sc.Step(`^o resultado de "([^"]+)" deve incluir a relação "([^"]+)" para "([^"]+)"$`, func(from, rel, to string) error {
		if err := s.statusIs(http.StatusOK); err != nil {
			return err
		}
		item := s.recalled(s.memories[from].String())
		if item == nil {
			return fmt.Errorf("%s not recalled: %s", from, s.resp.Body)
		}
		if !slices.ContainsFunc(item.Links, func(l link) bool { return l.is(rel, s.memories[from], s.memories[to]) }) {
			return fmt.Errorf("links of %s = %+v", from, item.Links)
		}
		return nil
	})

	// Contract completion.
	sc.Step(`^que o projeto "([^"]+)" também possui os espaços "([^"]+)" e "([^"]+)"$`, func(_, a, b string) error {
		return errors.Join(s.createSpace(a), s.createSpace(b))
	})
	sc.Step(`^listo os espaços com "limit" igual a (\d+)$`, func(limit int) {
		s.lastPath = fmt.Sprintf("/v1/spaces?limit=%d", limit)
		s.send(http.MethodGet, s.lastPath, s.token, nil)
	})
	sc.Step(`^listo a próxima página de espaços$`, func() { s.send(http.MethodGet, s.lastPath+"&cursor="+s.nextCursor, s.token, nil) })
	sc.Step(`^devem ser retornados (\d+) espaços e um "next_cursor"$`, func(n int) error { return s.page(n, true) })
	sc.Step(`^deve ser retornado (\d+) espaço e nenhum "next_cursor"$`, func(n int) error { return s.page(n, false) })
	sc.Step(`^listo os espaços do tipo "([^"]+)"$`, func(kind string) { s.send(http.MethodGet, "/v1/spaces?kind="+kind, s.token, nil) })
	sc.Step(`^somente o espaço "([^"]+)" deve ser retornado$`, func(externalID string) error {
		var p struct {
			Items []struct {
				ExternalID string `json:"external_id"`
			}
		}
		if err := json.Unmarshal(s.resp.Body.Bytes(), &p); err != nil {
			return err
		}
		if len(p.Items) != 1 || p.Items[0].ExternalID != externalID {
			return fmt.Errorf("spaces = %s", s.resp.Body)
		}
		return nil
	})
	sc.Step(`^altero "([^"]+)" dessa memória para "([^"]+)"$`, func(field, value string) {
		s.send(http.MethodPatch, "/v1/spaces/"+defaultSpace+"/memories/"+s.memoryID, s.token, map[string]any{field: value})
	})
	sc.Step(`^"([^"]+)" não deve estar presente no corpo$`, func(field string) error {
		body, err := s.body()
		if err != nil {
			return err
		}
		if v, ok := body[field]; ok && v != nil {
			return fmt.Errorf("%s = %v", field, v)
		}
		return nil
	})
	sc.Step(`^um recall somente com embedding do modelo "([^"]+)" não deve retorná-la$`, func(model string) error {
		s.recall(map[string]any{"query_embedding": embedding(1536, model)})
		if err := s.statusIs(http.StatusOK); err != nil {
			return err
		}
		if s.recalled(s.memoryID) != nil {
			return fmt.Errorf("memory with cleared embedding was recalled: %s", s.resp.Body)
		}
		return nil
	})

	sc.Step(`^envio um recall sem "query" e sem "query_embedding"$`, func() { s.recall(map[string]any{}) })
	sc.Step(`^que existem (\d+) memórias relevantes de aproximadamente (\d+) tokens cada$`, func(n, tokens int) error {
		content := strings.Repeat("relatório ", tokens*4/len([]rune("relatório ")))
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{"content": content}
		}
		s.send(http.MethodPost, "/v1/spaces/"+defaultSpace+"/memories/batch", s.token, map[string]any{"items": items})
		return s.statusIs(http.StatusOK)
	})
	sc.Step(`^envio um recall com "max_tokens" igual a (\d+)$`, func(max int) {
		s.recall(map[string]any{"query": "relatório", "max_tokens": max})
	})
	sc.Step(`^devem ser retornadas no máximo (\d+) memórias$`, func(n int) error {
		if err := s.statusIs(http.StatusOK); err != nil {
			return err
		}
		if got := len(s.recallItems()); got == 0 || got > n {
			return fmt.Errorf("%d memories returned, want 1..%d", got, n)
		}
		return nil
	})
}

func (s *apiScenario) createProjects(ctx context.Context, a, b string) error {
	for _, slug := range []string{a, b} {
		if _, err := s.adminDB.Exec(ctx, "DELETE FROM projects WHERE slug = $1", slug); err != nil {
			return err
		}
		var id uuid.UUID
		if err := s.adminDB.QueryRow(ctx, "INSERT INTO projects (slug, name) VALUES ($1, $2) RETURNING id", slug, slug).Scan(&id); err != nil {
			return err
		}
		s.projects[slug] = id
	}
	return nil
}

func (s *apiScenario) createSpaces(ctx context.Context, externalID string) error {
	for slug, projectID := range s.projects {
		var id uuid.UUID
		if err := s.adminDB.QueryRow(ctx,
			"INSERT INTO memory_spaces (project_id, external_id, name, kind) VALUES ($1, $2, $3, 'user') RETURNING id",
			projectID, externalID, externalID).Scan(&id); err != nil {
			return err
		}
		s.spaces[slug] = id
	}
	return nil
}

func (s *apiScenario) issueProjectKey(slug, scopes string) error {
	if err := s.issueKey(slug, strings.Join(strings.Fields(scopes), ",")); err != nil {
		return err
	}
	s.keys[slug] = s.token
	return nil
}

func (s *apiScenario) insertMemoryDirectly(ctx context.Context, label, slug string) error {
	var id uuid.UUID
	if err := s.adminDB.QueryRow(ctx,
		"INSERT INTO memories (project_id, space_id, content) VALUES ($1, $2, $3) RETURNING id",
		s.projects[slug], s.spaces[slug], "memória "+label).Scan(&id); err != nil {
		return err
	}
	s.memories[label] = id
	return nil
}

func (s *apiScenario) send(method, path, token string, body any) {
	var r *http.Request
	if body != nil {
		raw, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	s.resp = httptest.NewRecorder()
	s.target.ServeHTTP(s.resp, r)

	var created struct {
		Memory struct{ ID string }
	}
	if json.Unmarshal(s.resp.Body.Bytes(), &created) == nil && created.Memory.ID != "" {
		s.memoryID = created.Memory.ID
	}
}

func (s *apiScenario) retain(body map[string]any) {
	s.lastPath, s.lastBody = "/v1/spaces/"+defaultSpace+"/memories", body
	s.send(http.MethodPost, s.lastPath, s.token, body)
}

func (s *apiScenario) recall(body map[string]any) {
	s.send(http.MethodPost, "/v1/spaces/"+defaultSpace+"/recall", s.token, body)
}

type recallItem struct {
	Memory    struct{ ID string }
	MatchedBy []string `json:"matched_by"`
	Links     []link
}

type link struct {
	Source   uuid.UUID `json:"source_memory_id"`
	Target   uuid.UUID `json:"target_memory_id"`
	Relation string    `json:"relation_type"`
}

func (l link) is(rel string, source, target uuid.UUID) bool {
	return l.Relation == rel && l.Source == source && l.Target == target
}

// memoriesInSpaces creates memories "memória <label>" through the API,
// creating the spaces that do not exist yet.
func (s *apiScenario) memoriesInSpaces(labelA, spaceA, labelB, spaceB string) error {
	for _, m := range [][2]string{{labelA, spaceA}, {labelB, spaceB}} {
		s.send(http.MethodGet, "/v1/spaces/"+m[1], s.token, nil)
		if s.resp.Code == http.StatusNotFound {
			if err := s.createSpace(m[1]); err != nil {
				return err
			}
		}
		s.send(http.MethodPost, "/v1/spaces/"+m[1]+"/memories", s.token, map[string]any{"content": "memória " + m[0]})
		if err := s.statusIs(http.StatusCreated); err != nil {
			return err
		}
		s.memories[m[0]] = uuid.MustParse(s.memoryID)
	}
	return nil
}

func (s *apiScenario) createSpace(externalID string) error {
	_, err := s.adminDB.Exec(context.Background(),
		"INSERT INTO memory_spaces (project_id, external_id, name) VALUES ($1, $2, $3)",
		s.projects["assistente-financeiro"], externalID, externalID)
	return err
}

func (s *apiScenario) recallItems() []recallItem {
	var r struct{ Items []recallItem }
	_ = json.Unmarshal(s.resp.Body.Bytes(), &r)
	return r.Items
}

func (s *apiScenario) recalled(id string) *recallItem {
	for _, it := range s.recallItems() {
		if it.Memory.ID == id {
			return &it
		}
	}
	return nil
}

func (s *apiScenario) page(n int, wantNext bool) error {
	if err := s.statusIs(http.StatusOK); err != nil {
		return err
	}
	var p struct {
		Items      []json.RawMessage
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(s.resp.Body.Bytes(), &p); err != nil {
		return err
	}
	if len(p.Items) != n || (p.NextCursor != nil) != wantNext {
		return fmt.Errorf("page = %s", s.resp.Body)
	}
	if p.NextCursor != nil {
		s.nextCursor = *p.NextCursor
	}
	return nil
}

func (s *apiScenario) body() (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(s.resp.Body.Bytes(), &body); err != nil {
		return nil, fmt.Errorf("body %s: %w", s.resp.Body, err)
	}
	return body, nil
}

func (s *apiScenario) fieldEquals(field string, want any) error {
	body, err := s.body()
	if err != nil {
		return err
	}
	if body[field] != want {
		return fmt.Errorf("%s = %v, want %v (body %s)", field, body[field], want, s.resp.Body)
	}
	return nil
}

func (s *apiScenario) statusAndCode(status int, code string) error {
	if err := s.statusIs(status); err != nil {
		return err
	}
	var p struct{ Code string }
	if err := json.Unmarshal(s.resp.Body.Bytes(), &p); err != nil || p.Code != code {
		return fmt.Errorf("body %s, want code %s", s.resp.Body, code)
	}
	return nil
}

func embedding(dims int, model string) map[string]any {
	v := make([]float64, dims)
	for i := range v {
		v[i] = float64(i%7) / 10
	}
	return map[string]any{"model": model, "vector": v}
}

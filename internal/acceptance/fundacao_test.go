//go:build integration

// Package acceptance runs the features with godog against the API started
// in-process and a PostgreSQL container.
package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/admin"
	"github.com/Tomelin/extend-memory-ai/internal/app"
	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/config"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres/sqlcdb"
	"github.com/Tomelin/extend-memory-ai/internal/testsupport/pgtest"
	httptransport "github.com/Tomelin/extend-memory-ai/internal/transport/http"
)

const keyCacheTTL = time.Second

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// env is shared by all scenarios.
type env struct {
	db       *pgtest.DB
	cfg      config.API
	app      *app.App
	spans    *tracetest.InMemoryExporter
	logs     *syncBuffer
	adminDB  *pgxpool.Pool
	adminCfg config.Admin
}

// scenario holds per-scenario state.
type scenario struct {
	*env
	target     http.Handler
	cliErrs    []error
	startErr   error
	projects   map[string]uuid.UUID
	appConn    *pgx.Conn
	queryCount int64
	queryErr   error
	counts     map[uuid.UUID][]int64
	token      string
	stdout     string
	stderr     string
	resp       *httptest.ResponseRecorder
	traceID    trace.TraceID
	parentSpan trace.SpanID
	requestID  string
}

func TestFeatures(t *testing.T) {
	ctx := context.Background()
	db, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(context.Background()) })
	if err := db.MigrateUp(); err != nil {
		t.Fatal(err)
	}

	e := &env{
		db:       db,
		spans:    tracetest.NewInMemoryExporter(),
		logs:     &syncBuffer{},
		adminCfg: config.Admin{MigratorDatabaseURL: db.MigratorURL, AdminDatabaseURL: db.AdminURL},
		cfg: config.API{
			DatabaseURL:         db.AppURL,
			DBMaxConns:          5,
			EmbeddingDimensions: 1536,
			KeyCacheTTL:         keyCacheTTL,
			KeyNegativeCacheTTL: keyCacheTTL,
			MaxBodyBytes:        1 << 20,
			LogLevel:            slog.LevelInfo,
			LogFormat:           "json",
			Environment:         "test",
			ShutdownTimeout:     5 * time.Second,
			ServiceName:         "memory-api",
			ServiceVersion:      "test",
		},
	}
	e.adminDB, err = pgxpool.New(ctx, db.AdminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.adminDB.Close)
	e.app, err = app.New(ctx, e.cfg, app.Options{LogWriter: e.logs, SpanExporter: e.spans})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.app.Close(context.Background()) })

	suites := []struct {
		name, tags string
		init       func(*godog.ScenarioContext, *env)
	}{
		{name: "fundacao", init: initScenario},
		{name: "api_memoria", init: initAPIScenario},
	}
	for _, s := range suites {
		t.Run(s.name, func(t *testing.T) {
			suite := godog.TestSuite{
				Name:                s.name,
				ScenarioInitializer: func(sc *godog.ScenarioContext) { s.init(sc, e) },
				Options: &godog.Options{
					Format:   "pretty",
					Paths:    []string{"../../features/" + s.name + ".feature"},
					Tags:     s.tags,
					Strict:   true,
					TestingT: t,
				},
			}
			if suite.Run() != 0 {
				t.Fatal("acceptance scenarios failed")
			}
		})
	}
}

func initScenario(sc *godog.ScenarioContext, e *env) {
	s := &scenario{env: e, target: e.app.Handler, projects: map[string]uuid.UUID{}, counts: map[uuid.UUID][]int64{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if s.appConn != nil {
			_ = s.appConn.Close(ctx)
		}
		return ctx, errors.Join(err, s.db.MigrateUp())
	})

	// Migrations.
	sc.Step(`^um banco PostgreSQL limpo com o bootstrap aplicado$`, func() error {
		return s.cli("migrate", "down", "--all")
	})
	sc.Step(`^executo "memory-admin migrate up"(?: novamente)?$`, func() { s.cliErrs = append(s.cliErrs, s.cli("migrate", "up")) })
	sc.Step(`^executo "memory-admin migrate down" para todas as versões$`, func() {
		s.cliErrs = append(s.cliErrs, s.cli("migrate", "down", "--all"))
	})
	sc.Step(`^todos os comandos devem terminar sem erro$`, func() error { return errors.Join(s.cliErrs...) })
	sc.Step(`^a versão do schema não deve estar marcada como "dirty"$`, func() error {
		if err := s.cli("migrate", "version"); err != nil {
			return err
		}
		if !strings.Contains(s.stdout, "versão: 4 dirty: false") {
			return fmt.Errorf("unexpected version output %q", s.stdout)
		}
		return nil
	})
	sc.Step(`^que o banco está uma migração atrás da versão esperada pelo binário$`, func() error {
		return s.cli("migrate", "down", "1")
	})
	sc.Step(`^inicio o "memory-api"$`, func(ctx context.Context) {
		a, err := app.New(ctx, s.cfg, app.Options{})
		if err == nil {
			_ = a.Close(ctx)
		}
		s.startErr = err
	})
	sc.Step(`^o processo deve encerrar com erro informando a versão encontrada e a esperada$`, func() error {
		if s.startErr == nil || !strings.Contains(s.startErr.Error(), "found version 3, expected 4") {
			return fmt.Errorf("startup error = %w", s.startErr)
		}
		return nil
	})

	// Isolation.
	sc.Step(`^que existem memórias em dois projetos$`, s.seedTwoProjects)
	sc.Step(`^que duas requisições de projetos diferentes usam o mesmo pool de conexões$`, s.seedTwoProjects)
	sc.Step(`^que estou conectado como "memory_app"$`, func(ctx context.Context) (err error) {
		s.appConn, err = pgx.Connect(ctx, s.db.AppURL)
		return err
	})
	sc.Step(`^consulto a tabela "([^"]+)"(?: sem definir "app.current_project_id")?$`, func(ctx context.Context, table string) {
		s.queryErr = s.appConn.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&s.queryCount)
	})
	sc.Step(`^nenhuma linha deve ser retornada$`, func() error {
		if s.queryErr != nil || s.queryCount != 0 {
			return fmt.Errorf("count = %d, err = %w", s.queryCount, s.queryErr)
		}
		return nil
	})
	sc.Step(`^a consulta deve ser negada por falta de permissão$`, func() error {
		var pgErr *pgconn.PgError
		if !errors.As(s.queryErr, &pgErr) || pgErr.Code != "42501" {
			return fmt.Errorf("err = %w, want insufficient_privilege", s.queryErr)
		}
		return nil
	})
	sc.Step(`^ambas consultam memórias ao mesmo tempo$`, s.concurrentCounts)
	sc.Step(`^cada uma deve receber somente as memórias do próprio projeto$`, func() error {
		for name, want := range map[string]int64{"a": 2, "b": 3} {
			id := s.projects[name]
			for _, got := range s.counts[id] {
				if got != want {
					return fmt.Errorf("project %s saw %d memories, want %d", name, got, want)
				}
			}
		}
		return nil
	})

	// Keys.
	sc.Step(`^que criei o projeto "([^"]+)" pela CLI$`, func(ctx context.Context, slug string) error {
		if _, err := s.adminDB.Exec(ctx, "DELETE FROM projects WHERE slug = $1", slug); err != nil {
			return err
		}
		s.projects["cli"] = uuid.Nil
		return s.cli("project", "create", "--slug", slug, "--name", "Projeto "+slug)
	})
	sc.Step(`^que emiti uma chave com escopo "([^"]+)" pela CLI$`, func(scope string) error {
		return s.issueKey("assistente-financeiro", scope)
	})
	sc.Step(`^emito uma chave pela CLI$`, s.newProjectAndKey)
	sc.Step(`^a chave completa deve ser exibida uma única vez$`, func() error {
		if _, _, err := auth.ParseToken(s.token); err != nil {
			return fmt.Errorf("stdout %q is not a key", s.token)
		}
		if n := strings.Count(s.stdout+s.stderr, s.token); n != 1 {
			return fmt.Errorf("key printed %d times", n)
		}
		return nil
	})
	sc.Step(`^o banco deve armazenar somente o hash do segredo$`, func(ctx context.Context) error {
		prefix, secret, _ := auth.ParseToken(s.token)
		var hash []byte
		var row string
		if err := s.adminDB.QueryRow(ctx,
			"SELECT secret_hash, row_to_json(k)::text FROM project_api_keys k WHERE prefix = $1", prefix).Scan(&hash, &row); err != nil {
			return err
		}
		if !bytes.Equal(hash, auth.HashSecret(secret)) {
			return errors.New("stored hash does not match sha256(secret)")
		}
		if strings.Contains(row, secret) {
			return errors.New("secret stored in clear text")
		}
		return nil
	})
	sc.Step(`^que revoguei a chave pela CLI$`, func() error {
		if err := s.newProjectAndKey(); err != nil {
			return err
		}
		s.get("/v1/project", s.token)
		if s.resp.Code != http.StatusOK {
			return fmt.Errorf("key did not work before revocation: %d", s.resp.Code)
		}
		prefix, _, _ := auth.ParseToken(s.token)
		return s.cli("key", "revoke", "--prefix", prefix)
	})
	sc.Step(`^aguardo o TTL do cache de chaves$`, func() { time.Sleep(keyCacheTTL + 200*time.Millisecond) })

	// HTTP.
	sc.Step(`^envio "GET ([^"]+)" com essa chave$`, func(path string) { s.get(path, s.token) })
	sc.Step(`^envio "GET ([^"]+)" sem o cabeçalho Authorization$`, func(path string) { s.get(path, "") })
	sc.Step(`^envio "GET ([^"]+)"$`, func(path string) { s.get(path, "") })
	sc.Step(`^a resposta deve ter status (\d+)$`, s.statusIs)
	sc.Step(`^a resposta deve ter status (\d+) e código "([^"]+)"$`, func(status int, code string) error {
		if err := s.statusIs(status); err != nil {
			return err
		}
		var p struct{ Code string }
		if err := json.Unmarshal(s.resp.Body.Bytes(), &p); err != nil || p.Code != code {
			return fmt.Errorf("body %s, want code %s", s.resp.Body, code)
		}
		return nil
	})
	sc.Step(`^o Content-Type deve ser "([^"]+)"$`, func(ct string) error {
		if got := s.resp.Header().Get("Content-Type"); got != ct {
			return fmt.Errorf("Content-Type = %q", got)
		}
		return nil
	})
	sc.Step(`^o corpo deve conter o slug "([^"]+)"$`, func(slug string) error {
		var p struct{ Slug string }
		if err := json.Unmarshal(s.resp.Body.Bytes(), &p); err != nil || p.Slug != slug {
			return fmt.Errorf("body %s", s.resp.Body)
		}
		return nil
	})
	sc.Step(`^que o banco está indisponível$`, s.useUnavailableDatabase)

	// Observability.
	sc.Step(`^que enviei "GET /v1/project" com uma chave válida$`, func() error {
		if err := s.newProjectAndKey(); err != nil {
			return err
		}
		s.get("/v1/project", s.token)
		return s.statusIs(http.StatusOK)
	})
	sc.Step(`^envio "GET /v1/project" com o cabeçalho "traceparent" de um trace existente$`, func() error {
		if err := s.newProjectAndKey(); err != nil {
			return err
		}
		s.get("/v1/project", s.token)
		return nil
	})
	sc.Step(`^envio qualquer requisição$`, func() error {
		if err := s.newProjectAndKey(); err != nil {
			return err
		}
		s.get("/v1/project", s.token)
		return nil
	})
	sc.Step(`^consulto "/metrics" na porta interna$`, func() {
		s.resp = httptest.NewRecorder()
		s.app.MetricsHandler.ServeHTTP(s.resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	})
	sc.Step(`^deve existir a série "([^"]+)" com "([^"]+)" igual a "([^"]+)"$`, func(name, label, value string) error {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `(_bucket|_count|_sum)?\{[^}]*` + regexp.QuoteMeta(label+`="`+value+`"`))
		if !re.MatchString(s.resp.Body.String()) {
			return fmt.Errorf("series %s{%s=%q} not found", name, label, value)
		}
		return nil
	})
	sc.Step(`^devem existir métricas do pool de conexões do banco$`, func() error {
		if !strings.Contains(s.resp.Body.String(), "pgxpool_max_connections") {
			return errors.New("pgxpool metrics not found")
		}
		return nil
	})
	sc.Step(`^consulto os spans exportados$`, func() {})
	sc.Step(`^deve existir um span HTTP com rota "([^"]+)"$`, func(route string) error {
		_, err := s.httpSpan(route)
		return err
	})
	sc.Step(`^ele deve ter os spans filhos "([^"]+)" e "([^"]+)"$`, func(a, b string) error {
		root, err := s.httpSpan("/v1/project")
		if err != nil {
			return err
		}
		return errors.Join(s.descendant(root, a), s.descendant(root, b))
	})
	sc.Step(`^o span HTTP deve pertencer ao mesmo trace$`, func() error {
		root, err := s.httpSpan("/v1/project")
		if err != nil {
			return err
		}
		if root.Parent.SpanID() != s.parentSpan || !root.Parent.IsRemote() {
			return fmt.Errorf("HTTP span parent = %s, want remote %s", root.Parent.SpanID(), s.parentSpan)
		}
		return nil
	})
	sc.Step(`^a linha de log da requisição deve conter "([^"]+)", "([^"]+)" e "([^"]+)"$`, func(k1, k2, k3 string) error {
		line, err := s.requestLog()
		if err != nil {
			return err
		}
		for _, k := range []string{k1, k2, k3} {
			if v, _ := line[k].(string); v == "" {
				return fmt.Errorf("log line %v has no %s", line, k)
			}
		}
		if line["trace_id"] != s.traceID.String() {
			return fmt.Errorf("trace_id = %v, want %s", line["trace_id"], s.traceID)
		}
		return nil
	})
	sc.Step(`^não deve conter o segredo da chave de API$`, func() error {
		_, secret, _ := auth.ParseToken(s.token)
		if strings.Contains(s.logs.String(), secret) {
			return errors.New("log contains the API key secret")
		}
		return nil
	})
}

func (s *scenario) cli(args ...string) error {
	var stdout, stderr bytes.Buffer
	err := admin.Run(context.Background(), args, &stdout, &stderr, s.adminCfg)
	s.stdout, s.stderr = stdout.String(), stderr.String()
	if err != nil {
		return fmt.Errorf("memory-admin %s: %w (%s)", strings.Join(args, " "), err, s.stderr)
	}
	return nil
}

func (s *scenario) issueKey(slug, scopes string) error {
	if err := s.cli("key", "create", "--project", slug, "--name", "teste", "--scopes", scopes); err != nil {
		return err
	}
	s.token = strings.TrimSpace(s.stdout)
	return nil
}

func (s *scenario) newProjectAndKey() error {
	slug := "proj-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := s.cli("project", "create", "--slug", slug, "--name", slug); err != nil {
		return err
	}
	return s.issueKey(slug, "memories:read")
}

func (s *scenario) seedTwoProjects(ctx context.Context) error {
	for name, memories := range map[string]int{"a": 2, "b": 3} {
		slug := "iso-" + name + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
		var projectID, spaceID uuid.UUID
		if err := s.adminDB.QueryRow(ctx, "INSERT INTO projects (slug, name) VALUES ($1, $2) RETURNING id", slug, slug).Scan(&projectID); err != nil {
			return err
		}
		if err := s.adminDB.QueryRow(ctx,
			"INSERT INTO memory_spaces (project_id, external_id, name) VALUES ($1, 'user-1', 'Usuário') RETURNING id",
			projectID).Scan(&spaceID); err != nil {
			return err
		}
		for i := range memories {
			if _, err := s.adminDB.Exec(ctx,
				"INSERT INTO memories (project_id, space_id, content) VALUES ($1, $2, $3)",
				projectID, spaceID, fmt.Sprintf("memória %d do projeto %s", i, name)); err != nil {
				return err
			}
		}
		s.projects[name] = projectID
	}
	return nil
}

func (s *scenario) concurrentCounts(ctx context.Context) error {
	pool, err := postgres.NewPool(ctx, s.db.AppURL, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	store := postgres.New(pool)

	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"a", "b"} {
		id := s.projects[name]
		wg.Go(func() {
			for range 50 {
				err := store.WithProject(ctx, id, func(q *sqlcdb.Queries) error {
					n, err := q.CountMemories(ctx)
					if err != nil {
						return err
					}
					mu.Lock()
					s.counts[id] = append(s.counts[id], n)
					mu.Unlock()
					return nil
				})
				if err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	return <-errs
}

func (s *scenario) useUnavailableDatabase(ctx context.Context) error {
	pool, err := postgres.NewPool(ctx, "postgres://memory_app:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", 1)
	if err != nil {
		return err
	}
	store := postgres.New(pool)
	authn, err := auth.NewAuthenticator(store, auth.Options{})
	if err != nil {
		return err
	}
	s.target, err = httptransport.NewRouter(httptransport.Deps{
		ServiceName:   "memory-api",
		Logger:        slog.New(slog.DiscardHandler),
		Health:        store,
		Projects:      store,
		Authenticator: authn,
		MaxBodyBytes:  1 << 20,
	})
	return err
}

// get sends a request carrying a fresh client trace context and request ID,
// as an instrumented client would.
func (s *scenario) get(path, token string) {
	var tid [16]byte
	var sid [8]byte
	_, _ = rand.Read(tid[:])
	_, _ = rand.Read(sid[:])
	s.traceID, s.parentSpan = tid, sid
	s.requestID = "req-" + hex.EncodeToString(sid[:])

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", hex.EncodeToString(tid[:]), hex.EncodeToString(sid[:])))
	req.Header.Set("X-Request-ID", s.requestID)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.resp = httptest.NewRecorder()
	s.target.ServeHTTP(s.resp, req)
}

func (s *scenario) statusIs(status int) error {
	if s.resp.Code != status {
		return fmt.Errorf("status = %d, want %d (body %s)", s.resp.Code, status, s.resp.Body)
	}
	return nil
}

func (s *scenario) traceSpans() tracetest.SpanStubs {
	var out tracetest.SpanStubs
	for _, sp := range s.spans.GetSpans() {
		if sp.SpanContext.TraceID() == s.traceID {
			out = append(out, sp)
		}
	}
	return out
}

func (s *scenario) httpSpan(route string) (tracetest.SpanStub, error) {
	for _, sp := range s.traceSpans() {
		if sp.SpanKind != trace.SpanKindServer {
			continue
		}
		for _, attr := range sp.Attributes {
			if attr.Key == "http.route" && attr.Value.AsString() == route {
				return sp, nil
			}
		}
	}
	return tracetest.SpanStub{}, fmt.Errorf("no HTTP span with route %s in trace %s", route, s.traceID)
}

func (s *scenario) descendant(root tracetest.SpanStub, name string) error {
	spans := s.traceSpans()
	byID := map[trace.SpanID]tracetest.SpanStub{}
	for _, sp := range spans {
		byID[sp.SpanContext.SpanID()] = sp
	}
	for _, sp := range spans {
		if sp.Name != name {
			continue
		}
		for cur, ok := sp, true; ok; cur, ok = byID[cur.Parent.SpanID()] {
			if cur.Parent.SpanID() == root.SpanContext.SpanID() {
				return nil
			}
		}
	}
	return fmt.Errorf("span %q is not a descendant of %q", name, root.Name)
}

func (s *scenario) requestLog() (map[string]any, error) {
	for raw := range strings.SplitSeq(s.logs.String(), "\n") {
		var line map[string]any
		if json.Unmarshal([]byte(raw), &line) != nil {
			continue
		}
		if line["msg"] == "http request" && line["request_id"] == s.requestID {
			return line, nil
		}
	}
	return nil, fmt.Errorf("no log line for request %s", s.requestID)
}

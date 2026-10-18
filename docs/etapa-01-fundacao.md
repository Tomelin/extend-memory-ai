# Etapa 1 — Fundação da API

Esta etapa cria o esqueleto executável da API descrita em
[`arquitetura-api.md`](arquitetura-api.md), sem regras de negócio. Ao final, o
serviço sobe, conecta ao banco migrado, autentica uma chave emitida pela CLI e
já emite logs, métricas e spans. A primeira funcionalidade (`retain`/`recall`)
fica para a [Etapa 2](#próxima-etapa-etapa-2).

Critérios de aceitação: [`features/fundacao.feature`](../features/fundacao.feature).

## Objetivo

Deixar prontas e verificadas as decisões que são caras de mudar depois:

- schema versionado com golang-migrate e papéis do PostgreSQL corretos;
- geração de código com sqlc e o contexto de tenant (`WithProject`);
- servidor gin com a cadeia de middlewares definitiva;
- observabilidade (slog, OpenTelemetry, Prometheus) desde o primeiro endpoint;
- CLI `memory-admin` para operar o banco;
- CI que impede regressões em migrações, código gerado e testes.

## Fora do escopo

- endpoints de espaços, memórias, recall e relações (Etapa 2 em diante);
- busca semântica, RRF e orçamento de tokens;
- limite de taxa (o middleware é registrado, mas sem política ativa);
- imagem de produção publicada em registry e manifests de implantação.

## Entregáveis

### 1. Bootstrap do banco e migrações

**Decisão:** extensões e papéis são objetos do *cluster*, não do schema da
aplicação. Criá-los exige privilégios elevados e eles não devem ser revertidos
por um `down`. Por isso ficam fora das migrações:

```text
database/
  bootstrap/                       # executado por superusuário/DBA, uma vez por ambiente
    001-extensions.sql             # vector, unaccent, pg_trgm
    002-roles.sql                  # memory_migrator, memory_app, memory_admin
  migrations/                      # executado por memory_migrator via golang-migrate
    000001_init.up.sql             # configuração FTS portuguese_unaccent, tabelas, índices, gatilhos, RLS
    000001_init.down.sql
    000002_api_keys.up.sql         # project_api_keys + resolve_api_key (SECURITY DEFINER)
    000002_api_keys.down.sql
    000003_idempotency_hash.up.sql # memories.idempotency_hash bytea
    000003_idempotency_hash.down.sql
    000004_grants.up.sql           # GRANTs para memory_app e memory_admin
    000004_grants.down.sql
```

- O conteúdo de `database/initdb/001` a `003` é movido para `bootstrap/001` e
  `000001_init`; `database/initdb` deixa de existir.
- O `docker-compose.yaml` monta `database/bootstrap` em
  `/docker-entrypoint-initdb.d`. As migrações são aplicadas com
  `memory-admin migrate up`.
- Papéis em produção são criados sem senha no script; a credencial é definida
  pela infraestrutura (secret manager). Localmente, `002-roles.sql` define
  senhas de desenvolvimento.

Privilégios:

| Papel | Atributos | Privilégios concedidos |
| --- | --- | --- |
| `memory_migrator` | `LOGIN` | dono das tabelas; DDL |
| `memory_app` | `LOGIN`, **sem** `BYPASSRLS` | `SELECT, INSERT, UPDATE, DELETE` em `memory_spaces`, `memories`, `memory_links`; `SELECT` em `projects`; `EXECUTE` em `resolve_api_key`; nenhum acesso a `project_api_keys` |
| `memory_admin` | `LOGIN`, `BYPASSRLS` | DML em todas as tabelas, incluindo `project_api_keys`; usado somente pela CLI |

Como as tabelas usam `FORCE ROW LEVEL SECURITY`, o RLS vale também para o dono.

**Pronto quando:**

- `migrate up` → `migrate down -all` → `migrate up` executa sem erro em um banco
  limpo;
- com `memory_app`, uma consulta sem `app.current_project_id` retorna zero
  linhas, e `SELECT * FROM project_api_keys` é negado;
- `resolve_api_key` funciona com `memory_app`.

### 2. Módulo Go e geração de código

```text
go.mod                             # module github.com/Tomelin/extend-memory-ai
sqlc.yaml
Makefile
cmd/
  memory-api/main.go
  memory-admin/main.go
internal/
  config/
  domain/                          # Project, APIKey, Principal, erros de domínio
  auth/                            # formato emk_, geração, hash, comparação, cache
  store/postgres/
    pool.go                        # pgxpool + otelpgx
    tx.go                          # WithProject
    errors.go                      # SQLSTATE -> erros de domínio
    sqlcdb/                        # gerado
  transport/http/
    server.go                      # gin.New, middlewares, rotas, timeouts
    middleware/                    # requestid, slog, recover, metrics, bodylimit, auth, scope
    problem/                       # application/problem+json
    health.go                      # /healthz, /readyz
  observability/                   # Setup/Shutdown de traces, métricas e logger
database/
  queries/
    tenant.sql                     # SetCurrentProject
    auth.sql                       # ResolveAPIKey, TouchAPIKey
    projects.sql                   # GetProject, InsertProject (admin)
    api_keys.sql                   # InsertAPIKey, RevokeAPIKey (admin)
    health.sql                     # Ping, SchemaVersion
```

- Versão do Go: a estável mais recente na data da implementação, fixada no
  `go.mod` e no CI.
- `sqlc.yaml` usa `schema: [database/bootstrap/001-extensions.sql, database/migrations]`
  para que o sqlc conheça as extensões, e os *overrides* definidos na arquitetura.
- `oapi-codegen` é configurado para gin, mas nesta etapa gera apenas
  `GET /v1/project` e os modelos de erro.

Alvos do `Makefile`:

| Alvo | Ação |
| --- | --- |
| `make generate` | `sqlc generate` + `oapi-codegen` |
| `make lint` | `golangci-lint run`, `sqlc vet`, lint do OpenAPI |
| `make test` | testes unitários |
| `make test-integration` | testes com testcontainers e `godog` |
| `make up` / `make down` | `docker compose up -d` / `down` |
| `make migrate-up` | `go run ./cmd/memory-admin migrate up` |
| `make run` | `go run ./cmd/memory-api` |

**Pronto quando:** `make generate` não produz diff em um checkout limpo e
`go build ./...` passa.

### 3. Acesso ao banco e contexto de tenant

- `pgxpool` configurado por `MEMORY_DATABASE_URL` e `MEMORY_DB_MAX_CONNS`, com
  `otelpgx` como `QueryTracer` e `otelpgx.RecordStats` para métricas do pool.
- `WithProject(ctx, projectID, func(q *sqlcdb.Queries) error) error` como único
  ponto que abre transações de domínio.
- Tradução de erros: `23505` → conflito, `23503`/`23514` → validação,
  `40001`/`40P01` → repetível, timeout de contexto → indisponível.
- No boot, `memory-api` verifica: conexão, versão em `schema_migrations` igual à
  esperada pelo binário, `dirty = false` e dimensão da coluna `embedding` igual a
  `MEMORY_EMBEDDING_DIMENSIONS`. Qualquer divergência encerra o processo com log
  de erro.

**Pronto quando:** um teste de integração prova que duas transações
concorrentes de projetos diferentes no mesmo pool não enxergam dados uma da
outra.

### 4. Autenticação

- Formato `emk_<prefixo>_<segredo>`, geração com `crypto/rand`, armazenamento de
  `sha256(segredo)` e comparação com `subtle.ConstantTimeCompare`.
- Cache em memória por prefixo com TTL `MEMORY_KEY_CACHE_TTL`, incluindo cache
  negativo curto para prefixos inexistentes.
- Middleware `authenticate()` coloca `Principal{ProjectID, KeyID, Scopes}` no
  `context`; `requireScope(...)` por rota.
- Atualização de `last_used_at` assíncrona e amostrada.
- Primeiro endpoint autenticado: `GET /v1/project`.

**Pronto quando:** os cenários de autenticação de `fundacao.feature` passam.

### 5. Servidor HTTP (gin)

- `gin.New()`, `UseRawPath = true`, `UnescapePathValues = true`.
- Ordem dos middlewares: `otelgin` → request-id → log slog → recover
  (problem+json) → métricas HTTP → limite de corpo → (grupo `/v1`) autenticação.
- `NoRoute` e `NoMethod` retornam problem+json.
- `http.Server` com `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`,
  `IdleTimeout`; *graceful shutdown* em `SIGTERM`/`SIGINT` com prazo
  configurável, seguido de `observability.Shutdown` e fechamento do pool.
- Endpoints: `GET /healthz`, `GET /readyz` (executa `Ping` com timeout curto),
  `GET /v1/project`.
- `/metrics` em servidor separado, em `MEMORY_METRICS_ADDR`.

### 6. Observabilidade

Tudo descrito na seção [Observabilidade](arquitetura-api.md#observabilidade) da
arquitetura que não dependa de `retain`/`recall` é entregue agora:

- `observability.Setup(ctx, cfg) (shutdown func(context.Context) error, err error)`
  configurando `Resource`, `MeterProvider` com exportador Prometheus,
  `TracerProvider` OTLP, propagadores W3C e métricas de runtime;
- logger slog global com handler que injeta `trace_id`/`span_id`;
- métricas: `http.server.request.duration`, `http.server.active_requests`,
  `auth.failures`, `auth.key_cache.lookups`, `db.client.*` e runtime Go;
- spans: HTTP, `auth.resolve_key`, `store.WithProject` e um por consulta sqlc,
  nomeado pelo `-- name:`.

Ambiente local de observabilidade, adicionado ao `docker-compose.yaml`:

| Serviço | Imagem | Uso |
| --- | --- | --- |
| `jaeger` | `jaegertracing/all-in-one` | recebe OTLP em `4317` e mostra traces em `16686` |
| `prometheus` | `prom/prometheus` | coleta `/metrics` da API; UI em `9091` |

A API roda no host durante o desenvolvimento; o Prometheus alcança o host via
`host.docker.internal` (no Linux, `extra_hosts: ["host.docker.internal:host-gateway"]`).
Um coletor OpenTelemetry e o Grafana podem ser adicionados depois, sem mudar a
aplicação, porque ela só conhece OTLP e o formato Prometheus.

**Pronto quando:** após uma requisição a `GET /v1/project`, o trace com spans
HTTP, autenticação e banco aparece no Jaeger e
`http_server_request_duration_seconds` aparece no Prometheus com
`http_route="/v1/project"`.

### 7. CLI `memory-admin`

```text
memory-admin migrate up | down N | version | force V
memory-admin project create --slug <slug> --name <nome>
memory-admin key create --project <slug> --name <nome> --scopes memories:read,memories:write
memory-admin key revoke --prefix <prefixo>
memory-admin key list --project <slug>
```

- Migrações embutidas com `embed.FS` + `source/iofs` + driver `pgx/v5`.
- `migrate` conecta com `MEMORY_MIGRATOR_DATABASE_URL`; os demais comandos com
  `MEMORY_ADMIN_DATABASE_URL`.
- `key create` imprime a chave completa uma única vez e nunca a registra em log.

### 8. Testes e CI

Testes:

- unitários: formato e hash da chave, cache, tradução de erros, problem+json,
  middlewares;
- integração (testcontainers com `pgvector/pgvector:pg17`): aplica
  `bootstrap` + migrações, conecta como `memory_app` e valida RLS, `WithProject`
  e `resolve_api_key`;
- aceitação: `godog` executando `features/fundacao.feature` contra a API
  iniciada em processo.

GitHub Actions (`.github/workflows/ci.yaml`), em *pull requests* e na `main`:

| Job | Passos |
| --- | --- |
| `lint` | `golangci-lint`, `sqlc vet`, lint do `api/openapi.yaml` |
| `generate` | `make generate` e falha se `git diff` não estiver vazio |
| `migrations` | banco temporário: bootstrap → `up` → `down -all` → `up` |
| `test` | `make test` e `make test-integration` |
| `build` | `go build ./...` e build da imagem Docker (sem publicar) |

## Sequência sugerida de PRs

Cada PR é pequeno, revisável e deixa a `main` funcional:

1. **Banco:** `database/bootstrap`, migrações `000001`–`000004`, ajuste do
   `docker-compose.yaml`, remoção de `database/initdb`.
2. **Esqueleto Go:** `go.mod`, `Makefile`, `sqlc.yaml`, queries, `store/postgres`,
   `memory-admin` (migrate, project, key).
3. **Servidor e observabilidade:** gin, middlewares, health, observabilidade,
   autenticação, `GET /v1/project`, Jaeger e Prometheus no compose.
4. **CI:** workflow, testes de integração e `godog` com `fundacao.feature`.

## Definição de pronto da etapa

- todos os cenários de `features/fundacao.feature` passam no CI;
- um desenvolvedor novo executa `make up && make migrate-up`, cria projeto e
  chave pela CLI, chama `GET /v1/project` e vê o trace e as métricas localmente,
  seguindo apenas o README;
- o README descreve esse fluxo.

## Pontos a decidir durante a etapa

- configuração do `golangci-lint` (conjunto de linters);
- ferramenta de lint do OpenAPI no CI (ex.: Redocly CLI ou Spectral);
- Jaeger ou Grafana Tempo como visualizador local de traces;
- política de amostragem padrão em produção.

## Próxima etapa (Etapa 2)

Primeira fatia vertical guiada pelos cenários já existentes em
`features/api_memoria.feature`:

- `PUT/GET/DELETE /v1/spaces/{space}`;
- `POST /v1/spaces/{space}/memories` e `/batch` com idempotência;
- `GET` e `DELETE` de memórias;
- `POST /v1/spaces/{space}/recall` somente com busca textual;
- métricas `memory.retain.*` e `memory.recall.*`.

Busca semântica, RRF, orçamento de tokens e relações vêm na Etapa 3.

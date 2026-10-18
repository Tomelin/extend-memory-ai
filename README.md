# extend-memory-ai

Serviço enxuto de memória de longo prazo para aplicações de IA, com PostgreSQL
como fonte de verdade.

## Objetivo

O serviço recebe memórias já extraídas pelas aplicações clientes, preserva o
contexto de origem e permite recuperá-las por texto, significado e período. Ele
é inspirado nos conceitos de `retain` e `recall` do
[Hindsight](https://hindsight.vectorize.io/), mas o primeiro corte não tenta
reproduzir consolidação automática, reflexão por LLM ou páginas de conhecimento.

As decisões deste repositório são descritas por critérios de aceitação em
Gherkin, no idioma português (`# language: pt`). A especificação inicial está em
[`features/estrutura_memoria.feature`](features/estrutura_memoria.feature).

## Escopo da primeira etapa

- isolar completamente os dados de cada projeto de IA;
- subdividir um projeto em espaços de memória, como um usuário ou agente;
- armazenar memórias atômicas, e não transcrições inteiras;
- recuperar por busca textual, vetorial e filtros temporais;
- manter origem, metadados, expiração e relações explícitas entre memórias;
- possibilitar repetição segura de uma escrita por chave de idempotência.

Ficam fora deste primeiro corte: extração de fatos com LLM, geração de
embeddings, reranking, consolidação de observações e particionamento físico por
projeto. A autenticação HTTP é definida na
[arquitetura da API](docs/arquitetura-api.md#autenticação-e-autorização).

## Modelo de dados

```mermaid
erDiagram
    PROJECTS ||--o{ MEMORY_SPACES : possui
    MEMORY_SPACES ||--o{ MEMORIES : agrupa
    MEMORIES ||--o{ MEMORY_LINKS : origem
    MEMORIES ||--o{ MEMORY_LINKS : destino

    PROJECTS {
        uuid id PK
        varchar slug UK
        text name
        jsonb settings
    }
    MEMORY_SPACES {
        uuid id PK
        uuid project_id FK
        varchar external_id
        varchar kind
        jsonb metadata
    }
    MEMORIES {
        uuid id PK
        uuid project_id FK
        uuid space_id FK
        varchar memory_type
        text content
        vector embedding
        tsvector search_vector
        timestamptz occurred_at
        timestamptz expires_at
        jsonb metadata
    }
    MEMORY_LINKS {
        uuid project_id FK
        uuid space_id FK
        uuid source_memory_id FK
        uuid target_memory_id FK
        varchar relation_type
        real weight
    }
```

### `projects`

Representa o tenant: cada aplicação de IA tem um projeto. O `slug` é um nome
estável para APIs e o `id` UUID evita que identificadores sequenciais revelem
quantos projetos existem. `settings` guarda configuração pouco consultada sem
obrigar uma alteração de schema a cada nova opção.

O projeto é a fronteira de segurança. Todas as tabelas do domínio carregam
`project_id`, mesmo quando ele poderia ser inferido por um relacionamento. Essa
redundância é intencional: simplifica políticas de Row-Level Security (RLS),
índices e uma futura estratégia de particionamento.

### `memory_spaces`

É uma coleção isolada dentro do projeto, equivalente a um banco de memórias.
Pode representar um usuário, agente, equipe ou outra fronteira definida pelo
cliente. `external_id` permite usar o identificador do sistema de origem sem
acoplar a chave primária interna a ele, e é único somente dentro do projeto.

Separar espaço de projeto evita dois extremos: criar uma tabela para cada
cliente ou misturar todas as memórias de usuários diferentes em uma única
coleção lógica.

### `memories`

É a unidade atômica de conhecimento. Uma linha deve expressar um fato,
preferência, decisão, observação, resumo ou contexto que faça sentido quando
recuperado isoladamente.

- `content` é a representação legível e continua sendo a fonte de verdade;
- `embedding vector(1536)` oferece similaridade semântica por cosseno;
- `search_vector` é gerado pelo PostgreSQL com stemming em português e remoção
  de acentos, atendendo nomes e termos exatos que embeddings podem diluir;
- `occurred_at` descreve quando o fato aconteceu, enquanto `created_at`
  registra quando chegou ao serviço;
- `expires_at` dá suporte a memórias temporárias sem apagar seu histórico
  imediatamente;
- `source_type` e `source_ref` preservam rastreabilidade até a origem;
- `importance`, entre `0` e `1`, permite influenciar um ranking futuro;
- `idempotency_key` impede duplicação quando um cliente repete a mesma
  requisição;
- `metadata` recebe atributos específicos do cliente que não justificam colunas
  globais;
- `deleted_at` implementa exclusão lógica e permite auditoria ou recuperação.

A dimensão `1536` é uma decisão explícita da primeira versão. Todos os projetos
devem usar embeddings compatíveis; trocar de modelo por outro com dimensão
diferente exigirá uma migração ou uma tabela de embeddings separada.

### `memory_links`

Registra somente relações explícitas (`related_to`, `supports`, `contradicts`,
`supersedes` e `derived_from`). As chaves estrangeiras compostas impedem uma
relação entre projetos ou espaços diferentes. A tabela prepara navegação simples
por contexto relacionado sem introduzir um banco de grafos ou inferência
automática no MVP.

## Recuperação

A busca híbrida deverá executar, sempre dentro de um único `project_id` e
`space_id`:

1. busca lexical com Full Text Search;
2. busca semântica por distância de cosseno (`<=>`);
3. filtros de validade (`deleted_at IS NULL` e
   `expires_at IS NULL OR expires_at > now()`);
4. fusão das listas por posição, por exemplo Reciprocal Rank Fusion (RRF), em
   vez de somar scores que possuem escalas incompatíveis;
5. corte pelo orçamento de tokens da aplicação chamadora.

No início, a consulta vetorial pode ser exata. Um índice HNSW deve ser adicionado
quando volume e medições justificarem a troca de recall por velocidade. Em um
índice aproximado compartilhado, o filtro por tenant ocorre depois da varredura
e pode reduzir resultados; com muitos projetos, devem ser avaliados scans
iterativos, índices parciais ou particionamento. Consulte a documentação de
[filtros e multitenancy do pgvector](https://github.com/pgvector/pgvector#filtering).

## Isolamento entre projetos

O schema habilita e força RLS nas quatro tabelas de domínio. Toda operação da
API roda em uma transação aberta por `WithProject`, que define o projeto
corrente antes de qualquer consulta:

```sql
BEGIN;
SELECT set_config('app.current_project_id', '00000000-0000-0000-0000-000000000000', true);
-- consultas e alterações do projeto
COMMIT;
```

O terceiro argumento `true` limita o valor à transação (equivalente a
`SET LOCAL`, que não aceita parâmetros) e evita vazamento de tenant no pool de
conexões. A API conecta como `memory_app`, sem `SUPERUSER` e sem `BYPASSRLS`;
sem projeto corrente, ela não enxerga nenhuma linha. O projeto vem sempre da
chave de API, nunca da requisição.

## Arquivos de banco

| Caminho | Conteúdo | Quem executa |
| --- | --- | --- |
| `database/bootstrap/` | extensões (`vector`, `unaccent`, `pg_trgm`) e papéis `memory_migrator`, `memory_app`, `memory_admin` | superusuário/DBA, uma vez por ambiente |
| `database/local/` | senhas de desenvolvimento dos papéis | somente o `docker-compose.yaml` |
| `database/migrations/` | schema, índices, RLS, chaves de API e permissões ([golang-migrate](https://github.com/golang-migrate/migrate)) | `memory-admin migrate` |
| `database/queries/` | SQL usado pelo [sqlc](https://sqlc.dev) para gerar `internal/store/postgres/sqlcdb` | `task generate` |

## Executar localmente

Ferramentas: Go (versão do `go.mod`), Docker com Compose e
[Task](https://taskfile.dev) (`go install github.com/go-task/task/v3/cmd/task@latest`).
Para regenerar código e rodar o lint: `task tools` instala sqlc, oapi-codegen e
golangci-lint nas versões fixadas.

```bash
task up            # PostgreSQL (com bootstrap) e Prometheus
task migrate:up    # aplica as migrações como memory_migrator

task admin -- project create --slug assistente --name "Assistente"
TOKEN=$(task admin -- key create --project assistente --name local --scopes memories:read,memories:write)

# Copie endpoint e cabeçalho da integração OpenTelemetry do Grafana Cloud.
export OTEL_SERVICE_NAME=memory-api
export OTEL_RESOURCE_ATTRIBUTES="deployment.environment.name=development,service.namespace=extend-memory-ai"
export OTEL_TRACES_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_ENDPOINT="https://otlp-gateway-<regiao>.grafana.net/otlp"
export OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic%20<credencial-base64>"
export OTEL_TRACES_SAMPLER=parentbased_always_on

task run           # memory-api em :8080, métricas em :9090/metrics
curl -H "Authorization: Bearer $TOKEN" localhost:8080/v1/project
```

A chave completa é exibida apenas uma vez, na emissão. Os valores reais de
`OTEL_EXPORTER_OTLP_ENDPOINT` e `OTEL_EXPORTER_OTLP_HEADERS` devem vir do card
OpenTelemetry da sua stack no Grafana Cloud e nunca devem ser versionados.
Consulte a [especificação das variáveis de ambiente do OpenTelemetry](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/)
e o guia [Send data to the Grafana Cloud OTLP endpoint](https://grafana.com/docs/grafana-cloud/send-data/otlp/send-data-otlp/).
Depois da chamada:

- traces no Grafana Cloud, filtrados pelo serviço `memory-api`;
- métricas no Prometheus: <http://localhost:9091>, por exemplo
  `http_server_request_duration_seconds_count{http_route="/v1/project"}`.

Outros comandos (`task --list` mostra todos):

| Comando | Ação |
| --- | --- |
| `task generate` | `sqlc generate` + `oapi-codegen` |
| `task lint` | golangci-lint, `sqlc vet` e lint do OpenAPI (Redocly via Docker) |
| `task test` | testes unitários |
| `task test:integration` | testes de integração e `features/fundacao.feature` (godog + testcontainers; requer Docker) |
| `task test:migrations` | `up → down --all → up` no banco local |
| `task down` | derruba o ambiente local |

As credenciais do `docker-compose.yaml` e de `database/local` são
deliberadamente locais e não devem ser usadas em produção.

## API

A arquitetura da API HTTP (Go) que expõe `retain` e `recall` às aplicações
clientes está em [`docs/arquitetura-api.md`](docs/arquitetura-api.md). O
contrato público está em [`api/openapi.yaml`](api/openapi.yaml) e os critérios de
aceitação em [`features/api_memoria.feature`](features/api_memoria.feature).

O plano de implementação começa pela
[Etapa 1 — Fundação da API](docs/etapa-01-fundacao.md), seguida pela
[Etapa 2a — Entidades de domínio](docs/etapa-02a-entidades.md) e pela
[Etapa 2b — Endpoints](docs/etapa-02b-endpoints.md) e pela
[Etapa 3 — Busca híbrida e relações](docs/etapa-03-busca-hibrida.md) e pela
[Etapa 4 — Completar o contrato](docs/etapa-04-contrato.md) e pela
[Etapa 5 — Preparar para produção](docs/etapa-05-producao.md).

## Princípios para as próximas atividades

- cada mudança funcional começa com cenários Gherkin observáveis;
- dados de projetos diferentes nunca participam da mesma consulta ou relação;
- toda memória recuperada deve ser rastreável até sua origem;
- metadados flexíveis não substituem colunas para campos consultados com
  frequência;
- otimizações de índice são decididas por volume, latência e qualidade de
  recuperação medidos.

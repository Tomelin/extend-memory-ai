# Etapa 5 — Preparar para produção

Deixa o serviço pronto para rodar em produção: limite de requisições por chave,
imagem publicada, manifests de implantação, política de amostragem, expurgo de
dados e o critério para criar o índice vetorial.

Critérios de aceitação: [`features/producao.feature`](../features/producao.feature),
executado em `task test:integration`.

## Limite de requisições por chave

- Token bucket por chave de API (`golang.org/x/time/rate`), aplicado depois da
  autenticação no grupo `/v1`.
- `MEMORY_RATE_LIMIT_RPS` (padrão `50`; `0` desativa) e
  `MEMORY_RATE_LIMIT_BURST` (padrão `100`).
- Acima do limite: `429 rate_limited` com `Retry-After` em segundos e a métrica
  `ratelimit.rejected`.
- Os buckets ficam em memória em cada réplica: com N réplicas, o limite efetivo é
  até N × RPS. Buckets sem uso por 10 minutos são descartados. Se um limite
  global for necessário, o próximo passo é um store compartilhado (Redis), como
  previsto na arquitetura.

## Imagem e publicação

- `Dockerfile` existente: binários estáticos `memory-api` e `memory-admin` em
  `distroless/static:nonroot`, versão injetada por `-ldflags`.
- `.github/workflows/release.yaml`: em push no `main` e em tags `v*`, publica
  `ghcr.io/<owner>/<repo>` para `linux/amd64` e `linux/arm64`, com tags
  `main`, `latest`, `sha-<commit>` e a versão semântica da tag.

## Kubernetes (`deploy/kubernetes`)

| Arquivo | Conteúdo |
| --- | --- |
| `base/deployment.yaml` | init container `migrate` (`memory-admin migrate up` com o papel `memory_migrator`); 2 réplicas, rolling update sem indisponibilidade, `startupProbe`/`readinessProbe` em `/readyz`, `livenessProbe` em `/healthz`, usuário não root, sistema de arquivos somente leitura |
| `base/service.yaml` | porta `80` → `http` e `9090` → `metrics` |
| `base/httproute.yaml` | `HTTPRoute` (Gateway API) para o Envoy Gateway: gateways `https` e `http` do namespace `envoy-gateway`, prefixo `/` → Service `memory-api:80` |
| `base/hpa.yaml`, `base/pdb.yaml` | 2 a 10 réplicas por CPU (70%); ao menos 1 disponível |
| `base/configmap.yaml` | configuração não secreta, incluindo a amostragem |
| `base/purge-cronjob.yaml` | `memory-admin purge` diário às 03:30 |
| `base/secret.example.yaml` | exemplo do Secret `memory-api`; fora do `kustomization.yaml` |
| `overlays/with-bootstrap` | init container `bootstrap-extensions` antes do `migrate`, com o Secret `memory-api-bootstrap` |

Uso: crie o Secret `memory-api` e aplique `kubectl apply -k deploy/kubernetes/base`
(ou um overlay que troque a tag da imagem e os recursos).

### Migrações no init container

Cada pod roda `memory-admin migrate up` antes de iniciar a API. Réplicas que
sobem juntas são serializadas pelo advisory lock do golang-migrate; sem migração
pendente, o comando não faz nada. Só o init container recebe
`MEMORY_MIGRATOR_DATABASE_URL`; a API continua com `memory_app`.

Durante o rolling update os pods antigos convivem com o schema novo, então toda
migração precisa ser compatível com a versão anterior (expandir num release,
remover no seguinte).

### Extensões (`overlays/with-bootstrap`)

Para bancos em que nenhum DBA executou `database/bootstrap`, o overlay adiciona
o init container `bootstrap-extensions`, que roda `memory-admin bootstrap
extensions` (`CREATE EXTENSION IF NOT EXISTS vector, unaccent, pg_trgm`) antes
do `migrate`:

- usa o Secret separado `memory-api-bootstrap` (`MEMORY_BOOTSTRAP_DATABASE_URL`),
  montado só nesse container (exemplo em `secret.example.yaml` do overlay);
- `unaccent` e `pg_trgm` são *trusted* (PG 13+), mas `vector` normalmente exige
  superusuário (`rds_superuser` no RDS, `cloudsqlsuperuser` no Cloud SQL);
- não cria papéis: `memory_app`, `memory_migrator` e `memory_admin` continuam
  sendo criados uma vez com `database/bootstrap/002-roles.sql`.

Uso: `kubectl apply -k deploy/kubernetes/overlays/with-bootstrap`.

## Amostragem de traces

Produção usa `OTEL_TRACES_SAMPLER=parentbased_traceidratio` com
`OTEL_TRACES_SAMPLER_ARG=0.1`: respeita a decisão do cliente quando há
`traceparent` e amostra 10% das requisições que começam no serviço. Erros
continuam visíveis pelas métricas (`http.server.request.duration` com
`error.code`), que não dependem de amostragem.

## Expurgo

`memory-admin purge [--deleted-retention 720h] [--batch 1000]`, com o papel
`memory_admin`, em todos os projetos:

- remove fisicamente memórias expiradas;
- remove memórias excluídas logicamente há mais que a retenção (padrão 30 dias);
- apaga em lotes para manter os locks curtos; relações vão junto por cascata.

## Índice vetorial (HNSW)

A busca semântica continua exata. Criar `USING hnsw (embedding vector_cosine_ops)`
passa a valer quando, em produção:

- o p95 de `memory.recall.duration{strategy="semantic"}` ou `"hybrid"` passar
  de 200 ms; e
- os espaços consultados tiverem dezenas de milhares de memórias com embedding.

Como o índice é aproximado e o filtro por `space_id`/`embedding_model` reduz
muito os candidatos, a decisão deve comparar latência e recall antes e depois,
em uma migração própria (`CREATE INDEX CONCURRENTLY`).

## Definição de pronto da etapa

- cenários de `producao.feature` passam em `task test:integration`;
- `kustomize build deploy/kubernetes/base` gera os manifests sem erro;
- `task lint` e `task generate:check` sem apontamentos.

# Etapa 2b — Endpoints de espaços, memórias e recall textual

Segunda metade da Etapa 2. Expõe pela API as entidades da
[Etapa 2a](etapa-02a-entidades.md). Ao final, uma aplicação cliente cria um
espaço, grava memórias (uma a uma ou em lote, com idempotência), consulta,
lista, exclui e recupera memórias por busca textual.

Critérios de aceitação: [`features/api_memoria.feature`](../features/api_memoria.feature),
executado com `godog` em `task test:integration`. Os cenários marcados com
`@etapa-3` (busca semântica e relações) ficam fora desta execução.

## Camadas

```text
transport/http   handlers_memory.go, convert.go   DTO (api.gen.go) <-> domain, problem+json
usecase          usecase.go                       Prepare, spans app.*, métricas memory.*
store/postgres   memories.go                      WithProject + sqlc, sqlcdb <-> domain
database/queries spaces.sql, memories.sql
```

O pacote `internal/app` continua sendo a composição do processo; os casos de uso
ficam em `internal/usecase`, que não importa `gin`, `pgx` nem código gerado.

## Endpoints

| Método e rota | Escopo | Respostas |
| --- | --- | --- |
| `PUT /v1/spaces/{space}` | `spaces:write` | `201` criado, `200` atualizado |
| `GET /v1/spaces/{space}` | `memories:read` | `200`, `404 space_not_found` |
| `DELETE /v1/spaces/{space}` | `spaces:write` | `204`; remoção física, memórias em cascata |
| `POST /v1/spaces/{space}/memories` | `memories:write` | `201` + `Location`, `200` repetição, `409 idempotency_conflict` |
| `POST /v1/spaces/{space}/memories/batch` | `memories:write` | `200`, tudo ou nada |
| `GET /v1/spaces/{space}/memories` | `memories:read` | `200` com `next_cursor` |
| `GET /v1/spaces/{space}/memories/{id}` | `memories:read` | `200`, `404 memory_not_found` |
| `DELETE /v1/spaces/{space}/memories/{id}` | `memories:write` | `204`; exclusão lógica |
| `POST /v1/spaces/{space}/recall` | `memories:read` | `200` |

A listagem entrou nesta etapa porque os cenários de credencial usam
`GET .../memories`. `PATCH` de memória, `GET /v1/spaces` e relações continuam
para depois.

## Decisões

- **Corpo estrito.** O JSON é decodificado com `DisallowUnknownFields`
  (`additionalProperties: false`). JSON inválido ou campo desconhecido retorna
  `400 malformed_request`; regra de domínio violada retorna
  `422 validation_failed` com `errors[].field` no caminho do contrato
  (`embedding.vector`, `items[2].importance`).
- **Idempotência.** `InsertMemory` usa `ON CONFLICT ... DO NOTHING RETURNING`;
  sem linha, a memória existente é lida e `NewMemory.CheckReplay` compara o
  hash. Em lote, um conflito desfaz a transação inteira.
- **Recall textual.** `websearch_to_tsquery('portuguese_unaccent', query)`
  ordenado por `ts_rank_cd`, com os filtros do contrato. O score já usa RRF
  (`1 / (60 + posição)`), para não mudar de escala quando a busca semântica
  entrar. O orçamento `max_tokens` é aplicado em Go com a estimativa
  `ceil(runes / 4)`. `query_embedding` sozinho retorna `422`; junto de `query`,
  é ignorado até a Etapa 3.
- **Cursor.** `next_cursor` é base64url de `{"o": occurred_at, "i": id}`;
  cursor inválido retorna `400 malformed_request`.
- **Contrato.** O parâmetro `Space` recebeu `x-go-name: SpaceParam` para não
  colidir com o schema `Space` no `oapi-codegen`.

## Observabilidade

- Spans `app.UpsertSpace`, `app.GetSpace`, `app.DeleteSpace`, `app.Retain`,
  `app.RetainBatch`, `app.GetMemory`, `app.DeleteMemory`, `app.ListMemories` e
  `app.Recall` (eventos `lexical_candidates`, `returned`, `truncated`). Só
  erros inesperados marcam o span como falho.
- Métricas `memory.retain.items` (`result` = `created`, `replayed`, `conflict`;
  `memory_type`), `memory.recall.duration`, `memory.recall.candidates`
  (`strategy`), `memory.recall.results` e `memory.recall.truncated`.

## Definição de pronto da etapa

- os cenários de `api_memoria.feature` sem `@etapa-3` passam em
  `task test:integration`;
- `task lint` e `task generate:check` sem apontamentos.

## Próxima etapa (Etapa 3)

Busca semântica (pgvector, filtrando por `embedding_model`), fusão RRF das duas
listas, relações entre memórias (`include_links`) e remoção da tag `@etapa-3`
dos cenários correspondentes.

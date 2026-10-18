# Etapa 2a — Entidades de domínio

A primeira fatia vertical (`retain`/`recall`) foi dividida em duas partes:

- **Etapa 2a (esta):** entidades de domínio em `internal/domain`, com
  validações e valores padrão. Não há SQL, handler nem caso de uso novo.
- **Etapa 2b:** implementação dos endpoints sobre essas entidades (ver
  [Próxima etapa](#próxima-etapa-etapa-2b)).

A divisão permite revisar as regras de negócio isoladamente, antes que fiquem
espalhadas por queries e handlers.

Critérios de aceitação: [`features/entidades.feature`](../features/entidades.feature),
executado com `godog` em `task test` (sem banco, sem Docker).

## Princípios

- O domínio não importa `gin`, `pgx` nem código gerado (`sqlc`, `oapi-codegen`).
  Os DTOs HTTP e os modelos do sqlc são convertidos para as entidades nas
  camadas de borda.
- Cada entrada do cliente tem um tipo `*Input` com ponteiros para campos
  opcionais e um método `Prepare` que valida **todos** os campos e devolve a
  versão com padrões resolvidos. Assim o caso de uso nunca recebe dado inválido.
- Os limites espelham `api/openapi.yaml` e as restrições de `000001_init.up.sql`;
  o banco continua sendo a última palavra.
- Erros de validação são `*domain.ValidationError`, com a lista de campos no
  caminho JSON do contrato (`embedding.vector`, `items[2].importance`). O tipo
  satisfaz `errors.Is(err, domain.ErrValidation)`; a Etapa 2b só precisa
  traduzi-lo para o campo `errors` do problem+json.

## Entidades

| Arquivo | Tipos | Regras |
| --- | --- | --- |
| `errors.go` | `ValidationError`, `FieldError`, `ErrSpaceNotFound`, `ErrMemoryNotFound`, `ErrIdempotencyConflict` | os erros específicos continuam correspondendo a `ErrNotFound`/`ErrConflict` |
| `metadata.go` | `Metadata` | objeto JSON de até 8 KiB; ausente vira `{}` |
| `space.go` | `Space`, `SpaceInput`, `SpaceKind` | `external_id` não vazio e até 255 caracteres; `name` obrigatório; `kind` padrão `custom` |
| `memory.go` | `Memory`, `MemoryInput`, `NewMemory`, `MemoryType`, `Source`, `Embedding`, `RetainResult`, `PrepareBatch` | ver abaixo |
| `recall.go` | `RecallInput`, `RecallQuery`, `RecallFilters`, `RecallItem`, `RecallResult`, `MatchStrategy` | ver abaixo |

### Memória

- Padrões: `memory_type = fact`, `importance = 0.5`, `occurred_at` = instante da
  gravação, `metadata = {}`. Datas são normalizadas para UTC.
- `content` não vazio e até 16384 caracteres; `importance` entre 0 e 1;
  `expires_at` posterior a `occurred_at`; `source.type` até 50 caracteres;
  `idempotency_key` não vazia e até 255 caracteres.
- `embedding` com modelo informado e exatamente 1536 números finitos.
- `Memory.Active(now)` indica se a memória pode ser listada e recuperada (não
  excluída e não expirada).

### Idempotência

`NewMemory.IdempotencyHash` é o `sha256` da forma canônica dos campos gravados,
calculado somente quando há `idempotency_key`:

- o embedding fica de fora, pois pode variar por arredondamento;
- `occurred_at` só entra quando o cliente o enviou; caso contrário, uma
  repetição sem `occurred_at` em outro instante geraria um hash diferente e um
  falso conflito;
- as chaves de `metadata` são ordenadas, então a ordem enviada não importa.

`NewMemory.CheckReplay(existente)` devolve `ErrIdempotencyConflict` quando o
hash difere do armazenado em `memories.idempotency_hash`.

### Lote

`PrepareBatch` aceita de 1 a 100 itens e valida todos antes de devolver
qualquer resultado. Um item inválido rejeita o lote inteiro, com os campos
prefixados por `items[i]`.

### Recall

- Exige `query`, `query_embedding` ou ambos; `query` não vazia e até 2000
  caracteres.
- Padrões: `limit = 10` (1 a 100) e `candidate_limit = 50` (1 a 200);
  `max_tokens`, quando enviado, maior que zero.
- Filtros: tipos válidos, `occurred_before` posterior a `occurred_after`,
  `source_type` até 50 caracteres, `metadata` até 8 KiB.
- `RecallQuery.Lexical()`/`Semantic()` dizem quais estratégias executar;
  `RecallItem.MatchedBy()` deriva `matched_by` das posições em cada lista.

As entidades já modelam o contrato completo do recall (embedding, orçamento de
tokens e relações), mas a Etapa 2b implementa somente a busca textual.

## Fora do escopo

- queries sqlc, casos de uso, handlers e mapeamento problem+json;
- `Link`, `MemoryPatch` e paginação, que entram com seus endpoints.

## Definição de pronto da etapa

- todos os cenários de `features/entidades.feature` passam em `task test`;
- `task lint` sem apontamentos;
- nenhuma dependência de `gin`, `pgx` ou código gerado em `internal/domain`.

## Próxima etapa (Etapa 2b)

Detalhada em [`etapa-02b-endpoints.md`](etapa-02b-endpoints.md).

Implementação guiada pelos cenários de `features/api_memoria.feature`, usando as
entidades desta etapa:

- queries sqlc de espaços e memórias e adaptação `sqlcdb` ↔ `domain` em
  `store/postgres`;
- casos de uso `Spaces`, `Retain` e `Recall` (somente textual) em `internal/app`;
- `PUT/GET/DELETE /v1/spaces/{space}`, `POST .../memories` e `/batch`, `GET` e
  `DELETE` de memórias, `POST .../recall`;
- tradução de `ValidationError` e dos erros específicos para problem+json
  (`space_not_found`, `memory_not_found`, `idempotency_conflict`);
- métricas `memory.retain.*` e `memory.recall.*`.

Busca semântica, RRF, orçamento de tokens e relações continuam na Etapa 3.

# Etapa 4 — Completar o contrato

Com esta etapa, todas as operações de [`api/openapi.yaml`](../api/openapi.yaml)
estão implementadas. As que faltavam eram `listSpaces` e `updateMemory`; a
listagem de memórias já havia entrado na [Etapa 2b](etapa-02b-endpoints.md).

Critérios de aceitação: regra "O contrato está completo" de
[`api_memoria.feature`](../features/api_memoria.feature) e as regras de
alteração parcial e listagem de espaços de
[`entidades.feature`](../features/entidades.feature).

## `GET /v1/spaces`

- Escopo `memories:read`; filtro `kind`; `limit` de 1 a 100 (padrão 20).
- Paginação por *keyset* em `external_id` (ordem alfabética). `next_cursor` é o
  `external_id` do último item em base64url; cursor inválido retorna
  `400 malformed_request`.

## `PATCH /v1/spaces/{space}/memories/{id}`

- Escopo `memories:write`; corpo no formato JSON Merge Patch (RFC 7386). O
  handler lê o corpo como mapa para distinguir membro ausente de `null`, que
  uma decodificação em struct perderia.
- Campos mutáveis: `content`, `summary`, `embedding`, `importance`,
  `expires_at` e `metadata`.
- `memory_type`, `occurred_at`, `source` e `idempotency_key` são imutáveis e
  retornam `422 validation_failed` apontando o campo. Para corrigi-los, grava-se
  uma nova memória e uma relação `supersedes`. Campo desconhecido ou corpo vazio
  retorna `400 malformed_request`.
- `metadata` é mesclada recursivamente (membros `null` são removidos);
  `metadata: null` a esvazia.
- Se `content` mudar e o patch não trouxer `embedding`, o embedding e o modelo
  são removidos, para que a memória não seja encontrada por um vetor que já não
  a representa. `embedding: null` também remove.
- A regra fica em `domain.MemoryPatch.Apply`. O store lê a memória com
  `SELECT ... FOR UPDATE`, aplica o patch e grava na mesma transação
  (`Store.UpdateMemory`), evitando perda de atualização concorrente.
- `idempotency_hash` não muda: repetir o `retain` original devolve a memória já
  alterada com `created: false`.

## Observabilidade

Spans `app.ListSpaces` e `app.UpdateMemory`.

## Definição de pronto da etapa

- todos os cenários de `api_memoria.feature` e `entidades.feature` passam;
- `task lint` e `task generate:check` sem apontamentos.

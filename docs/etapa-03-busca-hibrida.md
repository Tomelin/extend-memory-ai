# Etapa 3 — Busca híbrida e relações

Completa o `recall` descrito em [`arquitetura-api.md`](arquitetura-api.md#recall-busca-híbrida)
e adiciona as relações explícitas entre memórias. Ao final, todos os cenários de
[`features/api_memoria.feature`](../features/api_memoria.feature) rodam no CI; a
tag `@etapa-3` deixa de existir.

## Busca semântica

- Query `RecallSemantic`: distância de cosseno (`embedding <=> $vetor`), busca
  exata (sem índice HNSW), restrita a `embedding_model = query_embedding.model`
  e aos mesmos filtros da busca textual.
- `query_embedding` sozinho é aceito; com `query`, as duas estratégias rodam
  na mesma transação (`Store.RecallCandidates`).
- O override `vector` não nulo do `sqlc.yaml` passou a usar `import`/`package`,
  pois a forma `github.com/pgvector/pgvector-go.Vector` gerava código inválido
  para parâmetros (`pgvector-go.Vector`).

## Fusão RRF

`domain.FuseRRF` junta as listas por memória e calcula
`score = Σ 1 / (60 + posição)`. Empates são resolvidos pela melhor posição e
pelo id, para que a ordem seja determinística. Em vez de uma única consulta SQL
(`RecallHybrid`, esboçada na arquitetura), a fusão é feita em Go: as duas
consultas ficam simples, cada uma com seu plano, e a regra fica testável sem
banco (`features/entidades.feature`).

O orçamento de tokens e o `limit` são aplicados depois da fusão.

## Relações

| Método e rota | Escopo | Respostas |
| --- | --- | --- |
| `POST /v1/spaces/{space}/memories/{id}/links` | `memories:write` | `201`; `409 link_conflict`; `422` se o destino não for memória ativa do mesmo espaço |
| `GET /v1/spaces/{space}/memories/{id}/links` | `memories:read` | `200`; `direction` = `outgoing`, `incoming`, `both` (padrão) e `relation_type` |
| `DELETE /v1/spaces/{space}/memories/{id}/links/{target_id}/{relation_type}` | `memories:write` | `204`; `404 not_found` |

- `domain.LinkInput.Prepare`: tipo válido, peso entre 0 e 1 (padrão 1), destino
  diferente da origem, `metadata` até 8 KiB.
- Relações cuja outra ponta foi excluída ou expirou não são listadas.
- `include_links=true` no recall adiciona as relações diretas (entrada e saída)
  de cada resultado, sem seguir relações recursivamente.

## Observabilidade

- Spans `app.CreateLink`, `app.ListLinks`, `app.DeleteLink`; `app.Recall` ganha
  o evento `semantic_candidates`.
- `memory.recall.candidates` com `strategy=semantic`; `memory.recall.duration`
  com `strategy` = `lexical`, `semantic` ou `hybrid`.

## Definição de pronto da etapa

- todos os cenários de `api_memoria.feature` e `entidades.feature` passam;
- `task lint` e `task generate:check` sem apontamentos.

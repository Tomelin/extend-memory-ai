# language: pt
Funcionalidade: Entidades de domínio da memória
  Como equipe que implementa a API de memória
  Quero que espaços, memórias e consultas de recall validem suas próprias regras
  Para que os casos de uso recebam somente dados válidos e com padrões resolvidos

  Regra: Um espaço é endereçado pelo identificador externo

    Cenário: Espaço sem tipo recebe o tipo padrão
      Dado um espaço "usuario-123" com nome "Usuário 123"
      Quando preparo o espaço
      Então a preparação deve ser aceita
      E o tipo do espaço deve ser "custom"

    Esquema do Cenário: Espaço inválido
      Dado um espaço "<external_id>" com nome "<nome>"
      E com "kind" igual a "<kind>"
      Quando preparo o espaço
      Então a preparação deve falhar no campo "<campo>"

      Exemplos:
        | external_id | nome        | kind     | campo |
        |             | Usuário 123 | user     | space |
        | usuario-123 |             | user     | name  |
        | usuario-123 | Usuário 123 | empresa  | kind  |

  Regra: Uma memória atômica respeita as restrições do banco antes de chegar a ele

    Cenário: Memória mínima recebe os valores padrão
      Dado uma memória com conteúdo "O usuário prefere relatórios em PDF"
      Quando preparo a memória para gravação
      Então a preparação deve ser aceita
      E o tipo da memória deve ser "fact"
      E a importância deve ser 0.5
      E o momento da ocorrência deve ser o instante da gravação

    Cenário: Memória preserva tipo, origem e ocorrência
      Dado uma memória com conteúdo "O usuário prefere relatórios em PDF"
      E com "memory_type" igual a "preference"
      E com "source.type" igual a "conversation"
      E com "source.ref" igual a "conversa-789"
      E com "occurred_at" igual a "2026-10-01T09:30:00-03:00"
      Quando preparo a memória para gravação
      Então a preparação deve ser aceita
      E o tipo da memória deve ser "preference"
      E a origem deve ser "conversation" com referência "conversa-789"
      E o momento da ocorrência deve ser "2026-10-01T12:30:00Z"

    Esquema do Cenário: Campo inválido é apontado na resposta
      Dado uma memória com conteúdo "O usuário prefere relatórios em PDF"
      E com "<campo>" igual a "<valor>"
      Quando preparo a memória para gravação
      Então a preparação deve falhar no campo "<campo>"

      Exemplos:
        | campo           | valor                |
        | content         |                      |
        | memory_type     | opiniao              |
        | importance      | 2                    |
        | importance      | -0.1                 |
        | idempotency_key |                      |
        | expires_at      | 2026-10-18T11:00:00Z |

    Cenário: Embedding com dimensão incorreta
      Dado uma memória com conteúdo "O usuário prefere relatórios em PDF"
      E com um embedding de 768 dimensões do modelo "modelo-a"
      Quando preparo a memória para gravação
      Então a preparação deve falhar no campo "embedding.vector"

    Cenário: Embedding sem modelo
      Dado uma memória com conteúdo "O usuário prefere relatórios em PDF"
      E com um embedding de 1536 dimensões do modelo ""
      Quando preparo a memória para gravação
      Então a preparação deve falhar no campo "embedding.model"

  Regra: A chave de idempotência distingue repetição de reaproveitamento

    Contexto:
      Dado uma memória com conteúdo "O usuário prefere relatórios em PDF"
      E com "idempotency_key" igual a "evento-456"
      E que a memória foi preparada e gravada

    Cenário: Repetição sem occurred_at em outro instante
      Quando preparo a mesma memória novamente uma hora depois
      Então a repetição deve ser aceita como idempotente

    Cenário: Repetição com embedding diferente
      Quando preparo a mesma memória novamente com um embedding de 1536 dimensões do modelo "modelo-a"
      Então a repetição deve ser aceita como idempotente

    Cenário: Reuso da chave com outro conteúdo
      Quando preparo a mesma memória novamente com "content" igual a "O usuário prefere planilhas"
      Então a repetição deve ser rejeitada por conflito de idempotência

  Regra: Um lote é validado como um todo

    Cenário: Item inválido rejeita o lote inteiro
      Dado um lote com 3 memórias válidas
      E o item 3 do lote tem "importance" igual a "2"
      Quando preparo o lote
      Então a preparação deve falhar no campo "items[2].importance"

    Esquema do Cenário: Tamanho do lote fora do limite
      Dado um lote com <quantidade> memórias válidas
      Quando preparo o lote
      Então a preparação deve falhar no campo "items"

      Exemplos:
        | quantidade |
        | 0          |
        | 101        |

  Regra: Uma consulta de recall precisa de texto ou embedding

    Cenário: Consulta textual recebe os limites padrão
      Dado uma consulta de recall com o texto "formato preferido do relatório"
      Quando preparo a consulta
      Então a preparação deve ser aceita
      E o limite deve ser 10 e o limite de candidatos 50
      E a consulta deve usar somente a busca "lexical"

    Cenário: Consulta somente com embedding
      Dado uma consulta de recall sem texto
      E com um embedding de 1536 dimensões do modelo "modelo-a"
      Quando preparo a consulta
      Então a preparação deve ser aceita
      E a consulta deve usar somente a busca "semantic"

    Cenário: Consulta sem texto nem embedding
      Dado uma consulta de recall sem texto
      Quando preparo a consulta
      Então a preparação deve falhar no campo "query"

    Esquema do Cenário: Parâmetro de consulta inválido
      Dado uma consulta de recall com o texto "ERR_CONNECTION_RESET"
      E com "<campo>" igual a "<valor>"
      Quando preparo a consulta
      Então a preparação deve falhar no campo "<campo>"

      Exemplos:
        | campo                | valor   |
        | limit                | 0       |
        | limit                | 101     |
        | candidate_limit      | 201     |
        | max_tokens           | 0       |
        | filters.memory_types | opiniao |

    Cenário: Período invertido
      Dado uma consulta de recall com o texto "ERR_CONNECTION_RESET"
      E com "filters.occurred_after" igual a "2026-10-18T00:00:00Z"
      E com "filters.occurred_before" igual a "2026-10-01T00:00:00Z"
      Quando preparo a consulta
      Então a preparação deve falhar no campo "filters.occurred_before"

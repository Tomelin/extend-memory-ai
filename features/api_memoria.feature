# language: pt
Funcionalidade: API HTTP do serviço de memória
  Como aplicação cliente
  Quero gravar e recuperar memórias por uma API autenticada
  Para usar a memória de longo prazo sem acessar dados de outro projeto

  Contexto:
    Dado que existem os projetos "assistente-financeiro" e "assistente-suporte"
    E que cada projeto possui o espaço "usuario-123"
    E que o projeto "assistente-financeiro" possui uma chave com escopos "memories:read memories:write"

  Regra: O projeto é determinado somente pela credencial

    Cenário: Requisição sem credencial
      Quando envio "GET /v1/spaces/usuario-123/memories" sem o cabeçalho Authorization
      Então a resposta deve ter status 401 e código "unauthenticated"

    Cenário: Chave revogada
      Dado que a chave do projeto "assistente-financeiro" foi revogada
      E que o cache de chaves expirou
      Quando envio "GET /v1/spaces/usuario-123/memories" com essa chave
      Então a resposta deve ter status 401 e código "unauthenticated"

    Cenário: Chave sem o escopo necessário
      Dado que estou autenticado com uma chave que possui apenas "memories:read"
      Quando gravo uma memória no espaço "usuario-123"
      Então a resposta deve ter status 403 e código "insufficient_scope"

    Cenário: Memória de outro projeto é indistinguível de inexistente
      Dado que existe a memória "M1" no projeto "assistente-suporte"
      E que estou autenticado no projeto "assistente-financeiro"
      Quando consulto a memória "M1" no espaço "usuario-123"
      Então a resposta deve ter status 404 e código "memory_not_found"

  Regra: Gravação (retain)

    Cenário: Gravar uma memória atômica com origem
      Dado que estou autenticado no projeto "assistente-financeiro"
      Quando envio "POST /v1/spaces/usuario-123/memories" com:
        | memory_type | content                             | source.type  | source.ref   |
        | preference  | O usuário prefere relatórios em PDF | conversation | conversa-789 |
      Então a resposta deve ter status 201
      E o corpo deve conter "created" igual a verdadeiro
      E a memória retornada deve informar a origem "conversation" e a referência "conversa-789"

    Cenário: Gravar em um espaço inexistente
      Dado que estou autenticado no projeto "assistente-financeiro"
      Quando gravo uma memória no espaço "usuario-inexistente"
      Então a resposta deve ter status 404 e código "space_not_found"

    Cenário: Repetição idempotente
      Dado que gravei a memória "O usuário prefere relatórios em PDF" com a chave de idempotência "evento-456"
      Quando repito exatamente a mesma requisição
      Então a resposta deve ter status 200
      E o corpo deve conter "created" igual a falso
      E o identificador deve ser o mesmo da primeira gravação

    Cenário: Reuso da chave de idempotência com outro conteúdo
      Dado que gravei a memória "O usuário prefere relatórios em PDF" com a chave de idempotência "evento-456"
      Quando gravo a memória "O usuário prefere planilhas" com a mesma chave
      Então a resposta deve ter status 409 e código "idempotency_conflict"

    Cenário: Embedding com dimensão incorreta
      Quando gravo uma memória com um embedding de 768 dimensões
      Então a resposta deve ter status 422 e código "validation_failed"
      E o erro deve apontar o campo "embedding.vector"

    Cenário: Lote é atômico
      Quando envio um lote com 3 memórias em que a terceira tem "importance" igual a 2
      Então a resposta deve ter status 422
      E nenhuma das 3 memórias deve ter sido gravada

  Regra: Espaços são criados explicitamente

    Cenário: Criar e atualizar um espaço
      Dado que o projeto "assistente-financeiro" possui uma chave com escopos "spaces:write memories:read"
      Quando envio "PUT /v1/spaces/usuario-456" com o nome "Usuário 456"
      Então a resposta deve ter status 201
      Quando envio "PUT /v1/spaces/usuario-456" com o nome "Usuário 456 (atualizado)"
      Então a resposta deve ter status 200
      E o corpo deve conter "name" igual a "Usuário 456 (atualizado)"

    Cenário: Remover um espaço apaga fisicamente suas memórias
      Dado que gravei a memória "O usuário prefere relatórios em PDF" com a chave de idempotência "evento-789"
      E que o projeto "assistente-financeiro" possui uma chave com escopos "spaces:write"
      Quando envio "DELETE /v1/spaces/usuario-123"
      Então a resposta deve ter status 204
      E nenhuma memória do projeto "assistente-financeiro" deve existir no banco

  Regra: Consulta e exclusão de memórias

    Cenário: Listar memórias com paginação
      Dado que existem 3 memórias gravadas no espaço "usuario-123"
      Quando listo as memórias do espaço "usuario-123" com "limit" igual a 2
      Então devem ser retornadas 2 memórias e um "next_cursor"
      Quando listo a próxima página
      Então deve ser retornada 1 memória e nenhum "next_cursor"

    Cenário: Memória excluída não é mais consultada nem recuperada
      Dado que gravei a memória "Erro ERR_CONNECTION_RESET ao exportar" com a chave de idempotência "evento-900"
      Quando excluo essa memória
      Então a resposta deve ter status 204
      E consultar essa memória deve retornar status 404 e código "memory_not_found"
      E um recall por "ERR_CONNECTION_RESET" não deve retorná-la

  Regra: Recuperação (recall)

    Cenário: Resultado informa por qual estratégia foi encontrado
      Dado que existe a memória "Erro ERR_CONNECTION_RESET ao exportar" com embedding do modelo "modelo-a"
      Quando envio "POST /v1/spaces/usuario-123/recall" com a consulta "ERR_CONNECTION_RESET" e um embedding do modelo "modelo-a"
      Então a memória deve ser retornada
      E "matched_by" deve conter "lexical"

    Cenário: Embeddings de modelos diferentes não são comparados
      Dado que existe uma memória com embedding do modelo "modelo-a" e sem termos em comum com a consulta
      Quando envio um recall somente com embedding do modelo "modelo-b"
      Então a memória não deve ser retornada

    Cenário: Recuperar por significado sem termos em comum
      Dado que existe uma memória com embedding do modelo "modelo-a" e sem termos em comum com a consulta
      Quando envio um recall somente com embedding do modelo "modelo-a"
      Então a memória deve ser retornada
      E "matched_by" deve conter "semantic"

    Cenário: Recall sem consulta textual nem embedding
      Quando envio um recall sem "query" e sem "query_embedding"
      Então a resposta deve ter status 422 e código "validation_failed"

    Cenário: Orçamento de tokens
      Dado que existem 20 memórias relevantes de aproximadamente 100 tokens cada
      Quando envio um recall com "max_tokens" igual a 350
      Então devem ser retornadas no máximo 3 memórias
      E o corpo deve conter "truncated" igual a verdadeiro

  Regra: Relações respeitam o espaço

    Cenário: Relação entre espaços diferentes é rejeitada
      Dado que existem as memórias "A" no espaço "usuario-123" e "B" no espaço "usuario-456"
      Quando crio a relação "supports" de "A" para "B" no espaço "usuario-123"
      Então a resposta deve ter status 422 e código "validation_failed"

    Cenário: Criar, listar e remover uma relação
      Dado que existem as memórias "A" no espaço "usuario-123" e "B" no espaço "usuario-123"
      Quando crio a relação "supports" de "A" para "B" no espaço "usuario-123"
      Então a resposta deve ter status 201
      Quando listo as relações de entrada de "B" no espaço "usuario-123"
      Então deve existir somente a relação "supports" de "A" para "B"
      Quando removo a relação "supports" de "A" para "B" no espaço "usuario-123"
      Então a resposta deve ter status 204

    Cenário: Relação duplicada
      Dado que existem as memórias "A" no espaço "usuario-123" e "B" no espaço "usuario-123"
      E que criei a relação "supports" de "A" para "B" no espaço "usuario-123"
      Quando crio a relação "supports" de "A" para "B" no espaço "usuario-123"
      Então a resposta deve ter status 409 e código "link_conflict"

    Cenário: Recall inclui as relações diretas
      Dado que existem as memórias "A" no espaço "usuario-123" e "B" no espaço "usuario-123"
      E que criei a relação "supports" de "A" para "B" no espaço "usuario-123"
      Quando envio um recall por "memória A" incluindo relações
      Então o resultado de "A" deve incluir a relação "supports" para "B"

  Regra: Erros seguem um formato único

    Cenário: Corpo de erro no padrão problem+json
      Quando qualquer requisição resulta em erro
      Então o Content-Type deve ser "application/problem+json"
      E o corpo deve conter "status", "code" e "request_id"

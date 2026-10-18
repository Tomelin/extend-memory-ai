# language: pt
Funcionalidade: Fundação da API de memória
  Como equipe que opera o serviço de memória
  Quero um esqueleto da API com banco migrado, autenticação e observabilidade
  Para construir as funcionalidades sobre uma base verificada

  Regra: O schema é versionado e reversível

    Cenário: Aplicar e reverter todas as migrações
      Dado um banco PostgreSQL limpo com o bootstrap aplicado
      Quando executo "memory-admin migrate up"
      E executo "memory-admin migrate down" para todas as versões
      E executo "memory-admin migrate up" novamente
      Então todos os comandos devem terminar sem erro
      E a versão do schema não deve estar marcada como "dirty"

    Cenário: API não inicia com schema desatualizado
      Dado que o banco está uma migração atrás da versão esperada pelo binário
      Quando inicio o "memory-api"
      Então o processo deve encerrar com erro informando a versão encontrada e a esperada

  Regra: O papel da aplicação respeita o isolamento

    Cenário: Consulta sem projeto corrente não retorna dados
      Dado que existem memórias em dois projetos
      E que estou conectado como "memory_app"
      Quando consulto a tabela "memories" sem definir "app.current_project_id"
      Então nenhuma linha deve ser retornada

    Cenário: Papel da aplicação não lê chaves diretamente
      Dado que estou conectado como "memory_app"
      Quando consulto a tabela "project_api_keys"
      Então a consulta deve ser negada por falta de permissão

    Cenário: Transações concorrentes não compartilham o projeto corrente
      Dado que duas requisições de projetos diferentes usam o mesmo pool de conexões
      Quando ambas consultam memórias ao mesmo tempo
      Então cada uma deve receber somente as memórias do próprio projeto

  Regra: Chaves são emitidas pela CLI e validadas pela API

    Cenário: Emitir uma chave e consultar o projeto
      Dado que criei o projeto "assistente-financeiro" pela CLI
      E que emiti uma chave com escopo "memories:read" pela CLI
      Quando envio "GET /v1/project" com essa chave
      Então a resposta deve ter status 200
      E o corpo deve conter o slug "assistente-financeiro"

    Cenário: Chave exibida apenas na emissão
      Quando emito uma chave pela CLI
      Então a chave completa deve ser exibida uma única vez
      E o banco deve armazenar somente o hash do segredo

    Cenário: Requisição sem credencial
      Quando envio "GET /v1/project" sem o cabeçalho Authorization
      Então a resposta deve ter status 401 e código "unauthenticated"
      E o Content-Type deve ser "application/problem+json"

    Cenário: Chave revogada deixa de funcionar após o TTL do cache
      Dado que revoguei a chave pela CLI
      Quando aguardo o TTL do cache de chaves
      E envio "GET /v1/project" com essa chave
      Então a resposta deve ter status 401 e código "unauthenticated"

  Regra: Saúde do serviço é observável

    Cenário: Liveness sem dependências
      Dado que o banco está indisponível
      Quando envio "GET /healthz"
      Então a resposta deve ter status 200

    Cenário: Readiness depende do banco
      Dado que o banco está indisponível
      Quando envio "GET /readyz"
      Então a resposta deve ter status 503 e código "unavailable"

    Cenário: Rota inexistente
      Quando envio "GET /v1/rota-inexistente"
      Então a resposta deve ter status 404
      E o Content-Type deve ser "application/problem+json"

  Regra: Métricas e spans existem desde o primeiro endpoint

    Cenário: Métricas HTTP expostas para o Prometheus
      Dado que enviei "GET /v1/project" com uma chave válida
      Quando consulto "/metrics" na porta interna
      Então deve existir a série "http_server_request_duration_seconds" com "http_route" igual a "/v1/project"
      E devem existir métricas do pool de conexões do banco

    Cenário: Trace cobre HTTP, autenticação e banco
      Dado que enviei "GET /v1/project" com uma chave válida
      Quando consulto os spans exportados
      Então deve existir um span HTTP com rota "/v1/project"
      E ele deve ter os spans filhos "auth.resolve_key" e "ResolveAPIKey"

    Cenário: Trace continua o contexto do cliente
      Quando envio "GET /v1/project" com o cabeçalho "traceparent" de um trace existente
      Então o span HTTP deve pertencer ao mesmo trace

    Cenário: Logs correlacionados com o trace
      Quando envio qualquer requisição
      Então a linha de log da requisição deve conter "request_id", "trace_id" e "span_id"
      E não deve conter o segredo da chave de API

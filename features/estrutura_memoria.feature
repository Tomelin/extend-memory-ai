# language: pt
Funcionalidade: Estrutura de memória para múltiplos projetos de IA
  Como aplicação cliente do serviço de memória
  Quero armazenar e recuperar memórias em um espaço isolado
  Para reutilizar contexto sem acessar informações de outro projeto ou usuário

  Contexto:
    Dado que existem os projetos "assistente-financeiro" e "assistente-suporte"
    E que cada projeto possui um espaço de memória com identificador externo "usuario-123"

  Regra: Toda operação respeita a fronteira do projeto e do espaço de memória

    Cenário: Armazenar uma memória atômica
      Dado que estou autenticado no projeto "assistente-financeiro"
      Quando armazeno a preferência "O usuário prefere relatórios em PDF" no espaço "usuario-123"
      Então a memória deve pertencer ao projeto "assistente-financeiro"
      E deve pertencer ao espaço "usuario-123"
      E deve preservar seu tipo, sua origem e quando o fato ocorreu

    Cenário: Não recuperar memória de outro projeto
      Dado que existe a memória "O usuário prefere relatórios em PDF" no projeto "assistente-financeiro"
      E que estou autenticado no projeto "assistente-suporte"
      Quando procuro por "formato preferido do relatório" no espaço "usuario-123"
      Então a memória do projeto "assistente-financeiro" não deve ser retornada

    Cenário: Não relacionar memórias de espaços diferentes
      Dado que existem duas memórias em espaços diferentes
      Quando tento criar uma relação entre essas memórias
      Então a relação deve ser rejeitada

  Regra: Escritas repetidas não criam duplicatas

    Cenário: Repetir uma requisição com a mesma chave de idempotência
      Dado que armazenei uma memória com a chave de idempotência "evento-456"
      Quando repito a escrita no mesmo projeto e espaço com a chave "evento-456"
      Então deve existir somente uma memória para essa chave

  Regra: A recuperação combina estratégias complementares

    Cenário: Recuperar por significado mesmo com palavras diferentes
      Dado que existe a memória "O usuário prefere relatórios em PDF"
      E que a memória possui um embedding
      Quando procuro por "qual é o formato favorito para documentos?"
      Então a memória deve ser candidata pela busca semântica

    Cenário: Recuperar termo exato pela busca textual
      Dado que existe uma memória contendo o código "ERR_CONNECTION_RESET"
      Quando procuro por "ERR_CONNECTION_RESET"
      Então a memória deve ser candidata pela busca textual

    Cenário: Ignorar memória expirada ou excluída
      Dado que existem memórias ativas, expiradas e excluídas no mesmo espaço
      Quando procuro memórias nesse espaço
      Então somente memórias ativas devem ser retornadas

  Regra: Resultados mantêm rastreabilidade

    Cenário: Identificar a origem da memória recuperada
      Dado que uma memória foi extraída da conversa "conversa-789"
      Quando essa memória é recuperada
      Então o resultado deve informar o tipo de origem e a referência "conversa-789"


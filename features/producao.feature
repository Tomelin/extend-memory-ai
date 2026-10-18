# language: pt
Funcionalidade: Operação em produção
  Como equipe que opera o serviço de memória
  Quero limitar o uso por chave e expurgar dados que não devem mais existir
  Para manter o serviço estável e cumprir a eliminação de dados pessoais

  Regra: Cada chave tem um limite de requisições

    Cenário: Chave acima do limite recebe 429 com Retry-After
      Dado que a API está configurada com limite de 1 requisição por segundo e rajada de 2
      E que estou autenticado com uma chave válida
      Quando envio 3 requisições seguidas com essa chave
      Então a última resposta deve ter status 429 e código "rate_limited"
      E o cabeçalho "Retry-After" deve estar presente
      E deve existir a métrica "ratelimit_rejected"

  Regra: O expurgo remove fisicamente o que não pode mais ser recuperado

    Cenário: Expurgar memórias expiradas e excluídas há mais tempo que a retenção
      Dado que existe uma memória ativa, uma expirada, uma excluída há 40 dias e uma excluída há 1 dia
      Quando executo "memory-admin purge --deleted-retention 720h"
      Então devem restar somente a memória ativa e a excluída há 1 dia
      E a saída deve informar 1 memória expirada e 1 excluída removidas

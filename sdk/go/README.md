# SDK Go do extend-memory-ai

Cliente Go pequeno e instrumentado com OpenTelemetry para a API HTTP do
`extend-memory-ai`. O pacote cobre projetos, espaços, `retain`, `recall`,
listagem e alteração de memórias e relações explícitas.

## Instalação

```bash
go get github.com/Tomelin/extend-memory-ai/sdk/go
```

O SDK é um módulo separado do servidor. Projetos clientes não carregam as
dependências de banco de dados, Gin ou observabilidade interna do serviço.

## Uso rápido

```go
package main

import (
	"context"
	"errors"
	"log"
	"time"

	memory "github.com/Tomelin/extend-memory-ai/sdk/go"
)

func main() {
	client, err := memory.NewClient(
		"https://memory.example.com",
		"emk_prefixo_segredo",
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err = client.UpsertSpace(ctx, "user-123", memory.SpaceInput{
		Name: "Usuário 123",
		Kind: memory.Ptr(memory.SpaceKindUser),
	})
	if err != nil {
		log.Fatal(err)
	}

	result, err := client.RetainMemory(ctx, "user-123", memory.MemoryInput{
		MemoryType:     memory.Ptr(memory.MemoryTypePreference),
		Content:        "O usuário prefere relatórios em PDF",
		IdempotencyKey: memory.Ptr("evento-456"),
		Source: &memory.Source{
			Type: "conversation",
			Ref:  "conversa-789",
		},
	})
	if err != nil {
		var apiErr *memory.APIError
		if errors.As(err, &apiErr) {
			log.Printf("API: status=%d code=%s request_id=%s",
				apiErr.StatusCode, apiErr.Problem.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}

	log.Printf("memória=%s criada=%t", result.Memory.ID, result.Created)

	recalled, err := client.Recall(ctx, "user-123", memory.RecallRequest{
		Query:     "Como o usuário prefere receber relatórios?",
		Limit:     5,
		MaxTokens: 1000,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, item := range recalled.Items {
		log.Printf("score=%.4f content=%s", item.Score, item.Memory.Content)
	}
}
```

## Atualizações parciais

`MemoryPatch` diferencia um membro omitido de um membro enviado como `null`.
Isso é necessário porque a API usa JSON Merge Patch:

```go
updated, err := client.UpdateMemory(ctx, "user-123", memoryID, memory.MemoryPatch{
	Content:        memory.Ptr("Conteúdo corrigido"),
	ClearSummary:   true,
	ClearEmbedding: true,
	Importance:     memory.Ptr(float32(0.8)),
})
```

Os controles `ClearSummary`, `ClearEmbedding`, `ClearExpiresAt` e
`ClearMetadata` enviam `null`. Campos com valor `nil` e sem o respectivo
controle são omitidos.

## Erros e rate limit

Respostas HTTP fora da faixa 2xx produzem `*memory.APIError`, com o documento
RFC 9457 em `Problem`, o `RequestID` e a duração de `RetryAfter` quando o
servidor responder com `429`:

```go
if memory.IsProblem(err, memory.ProblemCodeRateLimited) {
	var apiErr *memory.APIError
	errors.As(err, &apiErr)
	time.Sleep(apiErr.RetryAfter)
}
```

Para correlacionar uma chamada com os logs do serviço, envie um identificador
por contexto:

```go
ctx = memory.WithRequestID(ctx, "request-123")
```

## OpenTelemetry

Todos os métodos do cliente criam spans do tipo `client` usando o tracer
`extended_memory`, com nomes como `extended_memory.RetainMemory`. O SDK usa o
`TracerProvider` e o propagador globais configurados pela aplicação e injeta o
contexto distribuído na requisição HTTP. Erros de transporte, protocolo e API
são registrados no span correspondente.

## Testes

```bash
go test -race ./...
```

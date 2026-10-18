package memory_test

import (
	"context"
	"log"

	memory "github.com/Tomelin/extend-memory-ai/sdk/go"
)

func ExampleClient() {
	client, err := memory.NewClient("https://memory.example.com", "emk_prefix_secret")
	if err != nil {
		log.Fatal(err)
	}

	result, err := client.RetainMemory(context.Background(), "user-123", memory.MemoryInput{
		MemoryType:     memory.Ptr(memory.MemoryTypePreference),
		Content:        "O usuário prefere relatórios em PDF",
		IdempotencyKey: memory.Ptr("event-456"),
		Source: &memory.Source{
			Type: "conversation",
			Ref:  "conversation-789",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("memory %s (created=%t)", result.Memory.ID, result.Created)
}

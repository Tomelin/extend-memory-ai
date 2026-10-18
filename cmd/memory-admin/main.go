// Command memory-admin runs migrations and manages projects and API keys.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tomelin/extend-memory-ai/internal/admin"
	"github.com/Tomelin/extend-memory-ai/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := admin.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, config.LoadAdmin(os.LookupEnv))
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

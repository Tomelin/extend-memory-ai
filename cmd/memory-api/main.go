// Command memory-api serves the memory HTTP API.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tomelin/extend-memory-ai/internal/app"
	"github.com/Tomelin/extend-memory-ai/internal/config"
	"github.com/Tomelin/extend-memory-ai/internal/observability"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadAPI(os.LookupEnv)
	if err != nil {
		observability.NewLogger(os.Stderr, cfg.LogFormat, slog.LevelInfo).Error("invalid configuration", "error", err)
		return 1
	}
	cfg.ServiceVersion = version

	a, err := app.New(ctx, cfg, app.Options{LogWriter: os.Stdout})
	if err != nil {
		observability.NewLogger(os.Stderr, cfg.LogFormat, cfg.LogLevel).Error("startup failed", "error", err)
		return 1
	}
	slog.SetDefault(a.Logger)

	code := 0
	if err := a.Run(ctx); err != nil {
		a.Logger.Error("server stopped with error", "error", err)
		code = 1
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := a.Close(closeCtx); err != nil {
		a.Logger.Error("shutdown", "error", err)
		code = 1
	}
	return code
}

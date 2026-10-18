// Package app wires the memory-api process: telemetry, database, schema
// check, authentication and the HTTP servers.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/sync/errgroup"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/config"
	"github.com/Tomelin/extend-memory-ai/internal/observability"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres"
	httptransport "github.com/Tomelin/extend-memory-ai/internal/transport/http"
	"github.com/Tomelin/extend-memory-ai/internal/usecase"
)

const schemaCheckTimeout = 10 * time.Second

// Options customizes New, mainly for tests.
type Options struct {
	LogWriter    io.Writer
	SpanExporter sdktrace.SpanExporter
}

// App is an initialized memory-api.
type App struct {
	cfg            config.API
	Logger         *slog.Logger
	Handler        http.Handler
	MetricsHandler http.Handler
	pool           *pgxpool.Pool
	shutdownOTel   func(context.Context) error
}

// New initializes the application. It fails if the database schema does not
// match the version embedded in the binary.
func New(ctx context.Context, cfg config.API, opts Options) (_ *App, err error) {
	if opts.LogWriter == nil {
		opts.LogWriter = io.Discard
	}
	logger := observability.NewLogger(opts.LogWriter, cfg.LogFormat, cfg.LogLevel)
	if cfg.Environment != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	registry := prometheus.NewRegistry()
	shutdownOTel, err := observability.Setup(ctx, observability.Config{
		ServiceName:    cfg.ServiceName,
		ServiceVersion: cfg.ServiceVersion,
		Environment:    cfg.Environment,
		SpanExporter:   opts.SpanExporter,
		Registerer:     registry,
	})
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, Logger: logger, shutdownOTel: shutdownOTel}
	defer func() {
		if err != nil {
			err = errors.Join(err, a.Close(context.WithoutCancel(ctx)))
		}
	}()

	a.pool, err = postgres.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return nil, err
	}
	store := postgres.New(a.pool)

	expected, err := postgres.LatestVersion()
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, schemaCheckTimeout)
	defer cancel()
	if err := store.CheckSchema(checkCtx, expected, cfg.EmbeddingDimensions); err != nil {
		return nil, err
	}

	authn, err := auth.NewAuthenticator(store, auth.Options{
		CacheTTL:         cfg.KeyCacheTTL,
		NegativeCacheTTL: cfg.KeyNegativeCacheTTL,
		Logger:           logger,
	})
	if err != nil {
		return nil, err
	}
	memories, err := usecase.New(store, usecase.Options{})
	if err != nil {
		return nil, err
	}
	a.Handler, err = httptransport.NewRouter(httptransport.Deps{
		ServiceName:    cfg.ServiceName,
		Logger:         logger,
		Health:         store,
		Projects:       store,
		Memories:       memories,
		Authenticator:  authn,
		MaxBodyBytes:   cfg.MaxBodyBytes,
		RateLimitRPS:   cfg.RateLimitRPS,
		RateLimitBurst: cfg.RateLimitBurst,
	})
	if err != nil {
		return nil, err
	}
	a.MetricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return a, nil
}

// Run serves the API and the metrics endpoint until ctx is canceled, then
// shuts down gracefully within the configured timeout.
func (a *App) Run(ctx context.Context) error {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", a.MetricsHandler)
	servers := []*http.Server{
		newServer(a.cfg.HTTPAddr, a.Handler, ctx),
		newServer(a.cfg.MetricsAddr, metricsMux, ctx),
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, srv := range servers {
		g.Go(func() error {
			a.Logger.InfoContext(ctx, "listening", "addr", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve %s: %w", srv.Addr, err)
			}
			return nil
		})
	}
	g.Go(func() error {
		<-gctx.Done()
		a.Logger.InfoContext(ctx, "shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.ShutdownTimeout)
		defer cancel()
		var errs []error
		for _, srv := range servers {
			errs = append(errs, srv.Shutdown(shutdownCtx))
		}
		return errors.Join(errs...)
	})
	return g.Wait()
}

// Close flushes telemetry and closes the database pool.
func (a *App) Close(ctx context.Context) error {
	var err error
	if a.shutdownOTel != nil {
		err = a.shutdownOTel(ctx)
	}
	if a.pool != nil {
		a.pool.Close()
	}
	return err
}

func newServer(addr string, h http.Handler, ctx context.Context) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
}

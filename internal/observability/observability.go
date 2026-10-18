// Package observability configures OpenTelemetry traces and metrics (exported
// in the Prometheus format) and the slog logger.
package observability

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config configures Setup.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	// TracesExporter is "otlp" (endpoint and options from the standard
	// OTEL_EXPORTER_OTLP_* variables) or "none".
	TracesExporter string
	// SpanExporter, when set, replaces TracesExporter and exports spans
	// synchronously. Intended for tests.
	SpanExporter sdktrace.SpanExporter
	// Registerer receives the Prometheus collector for all metrics.
	Registerer prometheus.Registerer
}

// Setup installs the global tracer and meter providers and W3C propagators.
// Sampling follows OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG. The returned
// function flushes pending spans and must be called on shutdown.
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("deployment.environment.name", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	promExporter, err := otelprom.New(otelprom.WithRegisterer(cfg.Registerer))
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(promExporter))

	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	switch {
	case cfg.SpanExporter != nil:
		tpOpts = append(tpOpts, sdktrace.WithSyncer(cfg.SpanExporter))
	case cfg.TracesExporter == "otlp":
		exp, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("otlp exporter: %w", err), mp.Shutdown(ctx))
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)

	shutdown = func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, errors.Join(fmt.Errorf("runtime metrics: %w", err), shutdown(ctx))
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	return shutdown, nil
}

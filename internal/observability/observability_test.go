package observability

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSetupAcceptsGrafanaCloudOTLPEnvironment(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otlp-gateway.example.com/otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic%20dGVzdDp0ZXN0")
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_always_on")

	shutdown, err := Setup(context.Background(), Config{
		ServiceName:    "memory-api",
		ServiceVersion: "test",
		Environment:    "test",
		Registerer:     prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

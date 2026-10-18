package memory

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "extended_memory"

var tracer = otel.Tracer(tracerName)

func startSpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	return tracer.Start(
		ctx,
		tracerName+"."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
	)
}

func injectTraceContext(ctx context.Context, header http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

func recordSpanError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ActiveRequests maintains http.server.active_requests. The request duration
// histogram (http.server.request.duration) is recorded by otelgin.
func ActiveRequests(meter metric.Meter) (gin.HandlerFunc, error) {
	active, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithDescription("Number of in-flight HTTP requests."),
		metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		attrs := []attribute.KeyValue{attribute.String("http.request.method", c.Request.Method)}
		if route := c.FullPath(); route != "" {
			attrs = append(attrs, attribute.String("http.route", route))
		}
		opt := metric.WithAttributes(attrs...)
		ctx := c.Request.Context()
		active.Add(ctx, 1, opt)
		defer active.Add(ctx, -1, opt)
		c.Next()
	}, nil
}

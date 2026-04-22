package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns the application tracer.
func Tracer() trace.Tracer {
	return otel.Tracer("github.com/KarmaXP/mcp-gateway")
}

// StartSpan is a thin wrapper for child spans (O3: no argument payloads in attributes).
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, opts...)
}

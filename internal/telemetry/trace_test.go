package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestStartSpanUsesGlobalTracer(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := StartSpan(context.Background(), "unit.span")
	sc := trace.SpanContextFromContext(ctx)
	require.True(t, sc.IsValid())
	require.Equal(t, sc.TraceID(), span.SpanContext().TraceID())
	span.End()
}

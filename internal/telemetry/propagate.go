package telemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// ContextWithExtractedW3CTrace merges W3C Trace Context (traceparent, tracestate) from HTTP
// headers into parent. Safe when headers are absent; safe to call if otelhttp already extracted.
func ContextWithExtractedW3CTrace(parent context.Context, h http.Header) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if h == nil {
		return parent
	}
	return otel.GetTextMapPropagator().Extract(parent, propagation.HeaderCarrier(h))
}

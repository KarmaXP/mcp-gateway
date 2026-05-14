package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

func TraceHandler(inner slog.Handler) slog.Handler {
	return traceHandler{inner: inner}
}

type traceHandler struct {
	inner slog.Handler
}

func (t traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.inner.Enabled(ctx, level)
}

func (t traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
		r.AddAttrs(slog.String("span_id", sc.SpanID().String()))
	}
	return t.inner.Handle(ctx, r)
}

func (t traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{inner: t.inner.WithAttrs(attrs)}
}

func (t traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{inner: t.inner.WithGroup(name)}
}

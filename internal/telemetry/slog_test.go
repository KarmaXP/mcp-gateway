package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestTraceHandlerAddsTraceID(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	tr := tp.Tracer("t")
	ctx, span := tr.Start(context.Background(), "req")
	tid := span.SpanContext().TraceID().String()
	sid := span.SpanContext().SpanID().String()

	var buf bytes.Buffer
	h := TraceHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log := slog.New(h)
	log.InfoContext(ctx, "hello", "k", "v")
	span.End()

	var row map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &row))
	require.Equal(t, tid, row["trace_id"])
	require.Equal(t, sid, row["span_id"])
	require.Equal(t, "hello", row["msg"])
}

func TestTraceHandlerWithoutSpanNoTraceID(t *testing.T) {
	var buf bytes.Buffer
	h := TraceHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log := slog.New(h)
	log.InfoContext(context.Background(), "x")
	var row map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &row))
	_, ok := row["trace_id"]
	require.False(t, ok)
	_, ok = row["span_id"]
	require.False(t, ok)
}

func TestTraceHandlerWithAttrsAndGroupStillAddsTraceID(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	tr := tp.Tracer("t")
	ctx, span := tr.Start(context.Background(), "r")
	tid := span.SpanContext().TraceID().String()
	sid := span.SpanContext().SpanID().String()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	th := TraceHandler(inner).WithAttrs([]slog.Attr{slog.String("svc", "u")})
	log := slog.New(th)
	log.InfoContext(ctx, "wrapped", "k", "v")
	span.End()

	var row map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &row))
	require.Equal(t, tid, row["trace_id"])
	require.Equal(t, sid, row["span_id"])
	require.Equal(t, "wrapped", row["msg"])
	require.Equal(t, "u", row["svc"])
}

func TestTraceHandlerWithGroupChains(t *testing.T) {
	base := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := TraceHandler(base).WithGroup("outer")
	ctx := context.Background()
	require.True(t, h.Enabled(ctx, slog.LevelInfo))
}

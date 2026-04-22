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

	var buf bytes.Buffer
	h := TraceHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log := slog.New(h)
	log.InfoContext(ctx, "hello", "k", "v")
	span.End()

	var row map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &row))
	require.Equal(t, tid, row["trace_id"])
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
}

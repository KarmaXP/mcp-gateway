package multiplex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
)

func TestToolCallSpansNeverCarryArgumentValues(t *testing.T) {
	const canary = "CANARY-8f3ad91c"

	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b1.InputSchemaByTool = map[string]map[string]any{
		"echo": {
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"msg":   map[string]any{"type": "string", "pattern": "^ok$"},
				"count": map[string]any{"type": "integer", "maximum": 10},
			},
		},
	}

	tests := []struct {
		name      string
		arguments string
		leaked    string
	}{
		{name: "string rejected by pattern", arguments: `{"msg":"` + canary + `"}`, leaked: canary},
		{name: "number over maximum", arguments: `{"count":424242}`, leaked: "424,242"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(tp)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				_ = tp.Shutdown(context.Background())
			})

			a, err := New(context.Background(), []backend.Upstream{b1}, WithListTTL(0))
			require.NoError(t, err)
			_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
			_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

			params := json.RawMessage(`{"name":"alpha__echo","arguments":` + tc.arguments + `}`)
			resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
			require.NoError(t, err)
			require.NotNil(t, resp.Error, "the call must be rejected for the span to carry an error")

			var recorded []string
			for _, span := range rec.Ended() {
				for _, ev := range span.Events() {
					for _, at := range ev.Attributes {
						recorded = append(recorded, at.Value.String())
					}
				}
				for _, at := range span.Attributes() {
					recorded = append(recorded, at.Value.String())
				}
				recorded = append(recorded, span.Status().Description)
			}
			joined := strings.Join(recorded, "\n")
			require.NotEmpty(t, joined)
			require.NotContains(t, joined, tc.leaked, "argument values must never reach a span (SEC5)")
			require.Contains(t, joined, "schema validation failed at", "the sanitized message must still say where")
		})
	}
}

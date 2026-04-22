package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router"
)

func TestRecordSemanticRoutingNoPanic(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "semantic-metrics-test")
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	dec := &router.RoutingDecision{Outcome: router.OutcomeExact, LatencyMS: 3}
	RecordSemanticRouting(context.Background(), dec, nil)
	RecordSemanticRouting(context.Background(), dec, context.Canceled)
	RecordSemanticRouting(context.Background(), nil, nil)
}

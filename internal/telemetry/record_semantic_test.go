package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

	dec := SemanticRouting{Outcome: "exact", FallbackLayer: "exact", LatencyMS: 3}
	RecordSemanticRouting(context.Background(), dec, nil)
	RecordSemanticRouting(context.Background(), dec, context.Canceled)
	RecordSemanticRouting(context.Background(), SemanticRouting{}, nil)
}

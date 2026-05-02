package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestRecordInternalPhaseNoPanicAfterInit(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "mcp-gateway-internal-metric-test")
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	RecordInternalPhase(context.Background(), "tools/list", defaults.MetricInternalPhaseMux, 2*time.Millisecond)
	RecordInternalPhase(context.Background(), "", defaults.MetricInternalPhaseParse, 1*time.Millisecond)
	RecordInternalPhase(context.Background(), "tools/call", "unexpected_phase", 1*time.Millisecond)
}

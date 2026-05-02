package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestSecurityMetricsRecordersNoPanic(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "security-metrics-test")
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	ctx := context.Background()
	RecordPolicyDecision(ctx, defaults.MetricPolicyOutcomeDeny, defaults.MetricPolicyReasonNotInAllowList)
	RecordPolicyDecision(ctx, defaults.MetricPolicyOutcomeAllow, defaults.MetricPolicyReasonAllowListMatch)
	RecordPolicyDecision(ctx, "bogus-outcome", "unknown-reason")
	RecordJWKSLookup(ctx, defaults.MetricJWKSResultHit)
	RecordJWKSLookup(ctx, "bogus")
	RecordToolArgsValidation(ctx, defaults.MetricArgsStageLimits, defaults.MetricArgsResultPass)
	RecordToolArgsValidation(ctx, "bad-stage", "bad-result")
	RecordRateLimit(ctx, true)
	RecordRateLimit(ctx, false)
}

package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/KarmaXP/mcp-gateway/internal/router"
)

var (
	metricsReady     atomic.Bool
	semanticOutcomes metric.Int64Counter
	semanticDuration metric.Float64Histogram
)

func registerInstruments() error {
	m := otel.Meter("github.com/KarmaXP/mcp-gateway")
	var err error
	semanticOutcomes, err = m.Int64Counter("mcp.gateway.semantic_router.outcomes",
		metric.WithDescription("Semantic router resolutions by coarse result and outcome (bounded labels per O5)"))
	if err != nil {
		return fmt.Errorf("telemetry: semantic counter: %w", err)
	}
	semanticDuration, err = m.Float64Histogram("mcp.gateway.semantic_router.duration_seconds",
		metric.WithDescription("Semantic router decision latency (excludes backend tools/call)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("telemetry: semantic histogram: %w", err)
	}

	g, err := m.Int64ObservableGauge("mcp.gateway.active_sse_sessions",
		metric.WithDescription("Open MCP host SSE sessions"))
	if err != nil {
		return fmt.Errorf("telemetry: sessions gauge: %w", err)
	}
	_, err = m.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		obs.ObserveInt64(g, ActiveSessions.Load())
		return nil
	}, g)
	if err != nil {
		return fmt.Errorf("telemetry: sessions callback: %w", err)
	}
	metricsReady.Store(true)
	return nil
}

// RecordSemanticRouting emits router metrics from a completed decision (err may be non-nil).
func RecordSemanticRouting(ctx context.Context, dec *router.RoutingDecision, resolveErr error) {
	if !metricsReady.Load() || dec == nil {
		return
	}
	result := "miss"
	if resolveErr == nil {
		result = "hit"
	}
	outcome := string(dec.Outcome)
	if outcome == "" {
		outcome = "unknown"
	}
	attrs := []attribute.KeyValue{
		attribute.String("result", result),
		attribute.String("outcome", outcome),
	}
	semanticOutcomes.Add(ctx, 1, metric.WithAttributes(attrs...))
	if dec.LatencyMS > 0 {
		semanticDuration.Record(ctx, float64(dec.LatencyMS)/1000.0, metric.WithAttributes(attrs...))
	}
}

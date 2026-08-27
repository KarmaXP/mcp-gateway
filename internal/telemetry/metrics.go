package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

var (
	metricsReady          atomic.Bool
	indexedCatalogTools   atomic.Int64
	semanticOutcomes      metric.Int64Counter
	semanticDuration      metric.Float64Histogram
	policyDecisions       metric.Int64Counter
	jwksLookups           metric.Int64Counter
	toolArgsValidation    metric.Int64Counter
	rateLimitEvents       metric.Int64Counter
	payloadBytesReject    metric.Int64Counter
	internalDuration      metric.Float64Histogram
	sessionBroadcastDrops metric.Int64Counter
	sessionNotifyDrops    metric.Int64Counter
)

func registerInstruments() error {
	m := otel.Meter("github.com/KarmaXP/mcp-gateway")
	var err error
	semanticOutcomes, err = m.Int64Counter("mcp.gateway.semantic_router.outcomes",
		metric.WithDescription("Semantic router resolutions by hit/miss and outcome (low-cardinality labels)"))
	if err != nil {
		return fmt.Errorf("telemetry: semantic counter: %w", err)
	}
	semanticDuration, err = m.Float64Histogram("mcp.gateway.semantic_router.duration_seconds",
		metric.WithDescription("Semantic router decision latency (excludes upstream tools/call)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("telemetry: semantic histogram: %w", err)
	}

	policyDecisions, err = m.Int64Counter("mcp.gateway.policy.decisions",
		metric.WithDescription("Policy allow/deny decisions (bounded outcome and reason labels)"))
	if err != nil {
		return fmt.Errorf("telemetry: policy decisions counter: %w", err)
	}
	jwksLookups, err = m.Int64Counter("mcp.gateway.auth.jwks.lookups",
		metric.WithDescription("JWKS resolution: cache hit, refresh, or error class"))
	if err != nil {
		return fmt.Errorf("telemetry: jwks lookups counter: %w", err)
	}
	toolArgsValidation, err = m.Int64Counter("mcp.gateway.tool_args.validation",
		metric.WithDescription("tools/call argument checks: limits vs JSON Schema stage"))
	if err != nil {
		return fmt.Errorf("telemetry: tool args validation counter: %w", err)
	}
	rateLimitEvents, err = m.Int64Counter("mcp.gateway.ratelimit.events",
		metric.WithDescription("HTTP rate limiter: allowed vs throttled"))
	if err != nil {
		return fmt.Errorf("telemetry: ratelimit counter: %w", err)
	}
	payloadBytesReject, err = m.Int64Counter("mcp.gateway.payload.bytes_rejected",
		metric.WithDescription("Rejected oversized payloads: HTTP RPC body vs tools/call arguments (bounded reason)"))
	if err != nil {
		return fmt.Errorf("telemetry: payload bytes rejected counter: %w", err)
	}

	internalDuration, err = m.Float64Histogram("mcp.gateway.internal.duration_seconds",
		metric.WithDescription("Internal hop latency histogram by gateway phase (excludes upstream MCP I/O)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("telemetry: internal duration histogram: %w", err)
	}

	sessionBroadcastDrops, err = m.Int64Counter("mcp.gateway.session.broadcast_tasks_dropped",
		metric.WithDescription("SSE catalog notifications dropped because the broadcast work queue was full"))
	if err != nil {
		return fmt.Errorf("telemetry: broadcast tasks dropped counter: %w", err)
	}
	sessionNotifyDrops, err = m.Int64Counter("mcp.gateway.session.notifications_dropped",
		metric.WithDescription("SSE MCP notifications dropped because the per-session outbound buffer timed out"))
	if err != nil {
		return fmt.Errorf("telemetry: notification drops counter: %w", err)
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

	idx, err := m.Int64ObservableGauge("mcp.gateway.semantic_router.indexed_tools",
		metric.WithDescription("Tool count in the vector index after the last successful catalog reindex"))
	if err != nil {
		return fmt.Errorf("telemetry: indexed_tools gauge: %w", err)
	}
	_, err = m.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		obs.ObserveInt64(idx, indexedCatalogTools.Load())
		return nil
	}, idx)
	if err != nil {
		return fmt.Errorf("telemetry: indexed_tools callback: %w", err)
	}

	metricsReady.Store(true)
	return nil
}

func SetIndexedCatalogToolCount(n int64) {
	if n < 0 {
		n = 0
	}
	indexedCatalogTools.Store(n)
}

type SemanticRouting struct {
	Outcome       string
	FallbackLayer string
	LatencyMS     int64
}

func RecordSemanticRouting(ctx context.Context, dec SemanticRouting, resolveErr error) {
	if !metricsReady.Load() {
		return
	}
	result := "miss"
	if resolveErr == nil {
		result = "hit"
	}
	outcome := dec.Outcome
	if outcome == "" {
		outcome = "unknown"
	}
	layer := dec.FallbackLayer
	if layer == "" {
		layer = "unknown"
	}
	attrs := []attribute.KeyValue{
		attribute.String("result", result),
		attribute.String("outcome", outcome),
		attribute.String("layer", layer),
	}
	semanticOutcomes.Add(ctx, 1, metric.WithAttributes(attrs...))
	if dec.LatencyMS > 0 {
		semanticDuration.Record(ctx, float64(dec.LatencyMS)/defaults.MillisecondsPerSecond, metric.WithAttributes(attrs...))
	}
}

func RecordPolicyDecision(ctx context.Context, outcome, reason string) {
	if !metricsReady.Load() {
		return
	}
	if outcome != defaults.MetricPolicyOutcomeAllow && outcome != defaults.MetricPolicyOutcomeDeny {
		outcome = defaults.MetricPolicyOutcomeDeny
	}
	reason = normalizePolicyReason(reason)
	policyDecisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("reason", reason),
	))
}

func normalizePolicyReason(r string) string {
	switch r {
	case defaults.MetricPolicyReasonAllowListMatch,
		defaults.MetricPolicyReasonNotInAllowList,
		defaults.MetricPolicyReasonPolicyEvalFailed:
		return r
	default:
		return defaults.MetricPolicyReasonOther
	}
}

func RecordJWKSLookup(ctx context.Context, result string) {
	if !metricsReady.Load() {
		return
	}
	switch result {
	case defaults.MetricJWKSResultHit,
		defaults.MetricJWKSResultRefresh,
		defaults.MetricJWKSResultRefreshUnknownKid,
		defaults.MetricJWKSResultErrorFetch,
		defaults.MetricJWKSResultErrorMissingKid,
		defaults.MetricJWKSResultErrorUnknownKid:
	default:
		result = defaults.MetricJWKSResultErrorFetch
	}
	jwksLookups.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func RecordToolArgsValidation(ctx context.Context, stage, result string) {
	if !metricsReady.Load() {
		return
	}
	if stage != defaults.MetricArgsStageLimits && stage != defaults.MetricArgsStageSchema {
		stage = defaults.MetricArgsStageLimits
	}
	if result != defaults.MetricArgsResultPass && result != defaults.MetricArgsResultFail {
		result = defaults.MetricArgsResultFail
	}
	toolArgsValidation.Add(ctx, 1, metric.WithAttributes(
		attribute.String("stage", stage),
		attribute.String("result", result),
	))
}

func RecordRateLimit(ctx context.Context, allowed bool) {
	if !metricsReady.Load() {
		return
	}
	res := defaults.MetricRateLimitThrottled
	if allowed {
		res = defaults.MetricRateLimitAllowed
	}
	rateLimitEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("result", res)))
}

// RecordInternalPhase records gateway-only wall time for a phase (parse, security, router, mux).
// Method should be the JSON-RPC method when known; use defaults.MetricInternalMethodUnknown for pre-parse work.
func RecordInternalPhase(ctx context.Context, method, phase string, d time.Duration) {
	if !metricsReady.Load() || d < 0 {
		return
	}
	if method == "" {
		method = defaults.MetricInternalMethodUnknown
	}
	switch phase {
	case defaults.MetricInternalPhaseParse,
		defaults.MetricInternalPhaseSecurity,
		defaults.MetricInternalPhaseRouter,
		defaults.MetricInternalPhaseMux:
	default:
		phase = defaults.MetricInternalPhaseMux
	}
	internalDuration.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("phase", phase),
	))
}

func RecordPayloadBytesRejected(ctx context.Context, reason string) {
	if !metricsReady.Load() {
		return
	}
	switch reason {
	case defaults.MetricBytesRejectReasonHTTPBody, defaults.MetricBytesRejectReasonToolArgs:
	default:
		reason = defaults.MetricBytesRejectReasonHTTPBody
	}
	payloadBytesReject.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

func RecordBroadcastTaskDropped(ctx context.Context) {
	if !metricsReady.Load() {
		return
	}
	sessionBroadcastDrops.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", defaults.MetricSessionDropReasonBroadcastQueue),
	))
}

func RecordSessionNotificationDropped(ctx context.Context) {
	if !metricsReady.Load() {
		return
	}
	sessionNotifyDrops.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", defaults.MetricSessionDropReasonNotificationOutbound),
	))
}

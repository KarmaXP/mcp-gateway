package app

import (
	"context"

	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

type policyDecisionMetrics struct{}

func (policyDecisionMetrics) RecordPolicyDecision(ctx context.Context, outcome, reason string) {
	telemetry.RecordPolicyDecision(ctx, outcome, reason)
}

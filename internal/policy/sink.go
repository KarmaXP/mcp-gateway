package policy

import (
	"context"
	"log/slog"
	"time"
)

type AuditRecord struct {
	Outcome       string
	Reason        string
	ToolName      string
	SubjectSHA256 string
	PolicyVersion string
	At            time.Time
}

type AuditSink interface {
	Emit(ctx context.Context, rec AuditRecord) error
}

type DecisionMetrics interface {
	RecordPolicyDecision(ctx context.Context, outcome, reason string)
}

type SlogAuditSink struct{}

func (SlogAuditSink) Emit(ctx context.Context, rec AuditRecord) error {
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := []any{
		AuditMessageKey, true,
		"outcome", rec.Outcome,
		"reason", rec.Reason,
		"policy_version", rec.PolicyVersion,
		"subject_sha256_8", rec.SubjectSHA256,
	}
	if rec.ToolName != "" {
		attrs = append(attrs, "tool_name", rec.ToolName)
	}
	slog.InfoContext(ctx, "mcp policy decision", attrs...)
	return nil
}

type meteredAuditSink struct {
	inner   AuditSink
	metrics DecisionMetrics
}

func WithDecisionMetrics(inner AuditSink, metrics DecisionMetrics) AuditSink {
	if inner == nil || metrics == nil {
		return inner
	}
	return meteredAuditSink{inner: inner, metrics: metrics}
}

func (m meteredAuditSink) Emit(ctx context.Context, rec AuditRecord) error {
	if ctx == nil {
		ctx = context.Background()
	}
	err := m.inner.Emit(ctx, rec)
	m.metrics.RecordPolicyDecision(ctx, rec.Outcome, rec.Reason)
	return err
}

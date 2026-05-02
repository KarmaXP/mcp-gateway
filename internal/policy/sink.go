package policy

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

// AuditRecord is a bounded security audit event (SEC5: no tokens or argument bodies).
type AuditRecord struct {
	Outcome       string
	Reason        string
	ToolName      string
	SubjectSHA256 string
	PolicyVersion string
	At            time.Time
}

// AuditSink receives policy audit records (structured log, Kafka, syslog, etc.).
type AuditSink interface {
	Emit(ctx context.Context, rec AuditRecord) error
}

// SlogAuditSink is the default sink: structured slog + policy decision metrics.
type SlogAuditSink struct{}

// Emit implements AuditSink.
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
	telemetry.RecordPolicyDecision(ctx, rec.Outcome, rec.Reason)
	return nil
}

var (
	sinkMu     sync.RWMutex
	auditSinkI AuditSink = SlogAuditSink{}
)

// SetAuditSink swaps the process-wide audit sink (tests, future Kafka/syslog). Nil restores SlogAuditSink.
func SetAuditSink(s AuditSink) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	if s == nil {
		auditSinkI = SlogAuditSink{}
		return
	}
	auditSinkI = s
}

func currentAuditSink() AuditSink {
	sinkMu.RLock()
	defer sinkMu.RUnlock()
	if auditSinkI == nil {
		return SlogAuditSink{}
	}
	return auditSinkI
}

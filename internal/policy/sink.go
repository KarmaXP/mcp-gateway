package policy

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

// No tokens or argument payloads (SEC5).
type AuditRecord struct {
	Outcome       string
	Reason        string
	ToolName      string
	SubjectSHA256 string
	PolicyVersion string
	At            time.Time
}

// Emit must not write tokens or argument bodies (SEC5).
type AuditSink interface {
	Emit(ctx context.Context, rec AuditRecord) error
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
	telemetry.RecordPolicyDecision(ctx, rec.Outcome, rec.Reason)
	return nil
}

var (
	sinkMu          sync.RWMutex
	globalAuditSink AuditSink = SlogAuditSink{}
)

func SetAuditSink(s AuditSink) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	if s == nil {
		globalAuditSink = SlogAuditSink{}
		return
	}
	globalAuditSink = s
}

func currentAuditSink() AuditSink {
	sinkMu.RLock()
	defer sinkMu.RUnlock()
	if globalAuditSink == nil {
		return SlogAuditSink{}
	}
	return globalAuditSink
}

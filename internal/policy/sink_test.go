package policy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

type captureAuditSink struct {
	mu   sync.Mutex
	last AuditRecord
}

func (c *captureAuditSink) Emit(ctx context.Context, rec AuditRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = rec
	return nil
}

func (c *captureAuditSink) record() AuditRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func TestAuditorRecordHashesTheSubjectAndReachesItsOwnSink(t *testing.T) {
	t.Parallel()
	cap := &captureAuditSink{}

	NewAuditor(cap, []byte("test-pepper")).Record(context.Background(), Decision{
		Outcome:       "deny",
		Reason:        "not_in_allow_list",
		ToolName:      "k8s__x",
		SubjectID:     "sub-1",
		PolicyVersion: "pv2",
	})

	got := cap.record()
	require.Equal(t, "deny", got.Outcome)
	require.Equal(t, "not_in_allow_list", got.Reason)
	require.Equal(t, "k8s__x", got.ToolName)
	require.Len(t, got.SubjectSHA256, 16)
	require.Equal(t, "pv2", got.PolicyVersion)
	require.NotEqual(t, "sub-1", got.SubjectSHA256, "the raw subject must never reach a sink")
	require.WithinDuration(t, time.Now(), got.At, 2*time.Second)
}

func TestNilAuditorRecordsNothing(t *testing.T) {
	t.Parallel()
	var auditor *Auditor
	require.NotPanics(t, func() {
		auditor.Record(context.Background(), Decision{Outcome: "deny"})
	})
	require.Nil(t, NewAuditor(nil, nil), "no sink means no auditor, not an auditor that drops records")
}

func TestSlogAuditSink_EmitWithTelemetryNoPanic(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := telemetry.Init(context.Background(), "policy-slog-sink-test")
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	ctx := context.Background()
	rec := AuditRecord{
		Outcome:       "deny",
		Reason:        "not_in_allow_list",
		ToolName:      "p__t",
		SubjectSHA256: "abcd1234",
		PolicyVersion: "v9",
		At:            time.Now(),
	}
	require.NoError(t, SlogAuditSink{}.Emit(ctx, rec))
}

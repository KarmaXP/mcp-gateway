package policy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func TestSetAuditSink_DelegatesLogAudit(t *testing.T) {
	cap := &captureAuditSink{}
	SetAuditSink(cap)
	t.Cleanup(func() { SetAuditSink(nil) })

	ctx := context.Background()
	LogAudit(ctx, "deny", "not_in_allow_list", "k8s__x", "sub-1", "pv2")

	got := cap.record()
	require.Equal(t, "deny", got.Outcome)
	require.Equal(t, "not_in_allow_list", got.Reason)
	require.Equal(t, "k8s__x", got.ToolName)
	require.Equal(t, HashSubject("sub-1"), got.SubjectSHA256)
	require.Equal(t, "pv2", got.PolicyVersion)
	require.WithinDuration(t, time.Now(), got.At, 2*time.Second)
}

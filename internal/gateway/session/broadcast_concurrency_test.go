package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestBroadcastNotificationBoundedWorkerConcurrency(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	mpx, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	sm := NewSessionManager(mpx)

	const n = 80
	for i := 0; i < n; i++ {
		s := NewSession(context.Background(), "slow", mpx, nil)
		for j := 0; j < defaults.SessionOutboundChannelSize; j++ {
			s.out <- []byte("blocked")
		}
		sm.mu.Lock()
		sm.sessions[s.id] = s
		sm.mu.Unlock()
	}

	var peak atomic.Int32
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if v := sm.broadcastPeak.Load(); v > peak.Load() {
					peak.Store(v)
				}
				if v := sm.broadcastInflight.Load(); v > peak.Load() {
					peak.Store(v)
				}
			}
		}
	}()

	start := time.Now()
	sm.BroadcastNotification(&rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/tools/list_changed"})
	elapsed := time.Since(start)
	close(done)

	require.Less(t, elapsed, 500*time.Millisecond)
	require.LessOrEqual(t, peak.Load(), int32(defaults.SessionBroadcastMaxConcurrency))
	require.LessOrEqual(t, sm.broadcastPeak.Load(), int32(defaults.SessionBroadcastMaxConcurrency))
}

func TestEnqueueBroadcastTaskDropsWhenWorkQueueFull(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	mpx, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	// No broadcast workers: keep the work queue full deterministically.
	sm := &SessionManager{
		sessions:       make(map[string]*Session),
		multiplexer:    mpx,
		broadcastTasks: make(chan broadcastTask, defaults.SessionBroadcastWorkQueueSize),
	}

	s := NewSession(context.Background(), "s1", mpx, nil)
	req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/tools/list_changed"}
	for i := 0; i < defaults.SessionBroadcastWorkQueueSize; i++ {
		sm.broadcastTasks <- broadcastTask{sess: s, req: req}
	}

	before := sm.BroadcastTasksDropped()
	require.False(t, sm.enqueueBroadcastTask(s, req))
	require.Equal(t, uint64(1), sm.BroadcastTasksDropped()-before)
}

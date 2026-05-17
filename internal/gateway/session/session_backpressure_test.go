package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestEnqueueResponseTimesOutWhenOutboundFull(t *testing.T) {
	s := NewSession(context.Background(), "full", nil, nil)
	for i := 0; i < defaults.SessionOutboundChannelSize; i++ {
		s.out <- []byte("blocked")
	}
	err := s.EnqueueResponse(rpc.NewResult(json.RawMessage(`1`), json.RawMessage(`{}`)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "outbound buffer full")
	require.Equal(t, uint64(1), s.DroppedOutbound())
}

func TestBroadcastNotificationReturnsWithoutWaitingForSlowConsumer(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	mpx, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	sm := NewSessionManager(mpx)

	slow := NewSession(context.Background(), "slow", mpx, nil)
	for i := 0; i < defaults.SessionOutboundChannelSize; i++ {
		slow.out <- []byte("x")
	}
	sm.mu.Lock()
	sm.sessions[slow.id] = slow
	sm.mu.Unlock()

	fast := NewSession(context.Background(), "fast", mpx, nil)
	sm.mu.Lock()
	sm.sessions[fast.id] = fast
	sm.mu.Unlock()

	start := time.Now()
	sm.BroadcastNotification(&rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/tools/list_changed"})
	elapsed := time.Since(start)
	require.Less(t, elapsed, 500*time.Millisecond)
}

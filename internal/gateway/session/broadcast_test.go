package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestSessionManagerBroadcastNotification(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	sm := NewSessionManager(mpx)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, err := sm.Create(ctx)
	require.NoError(t, err)
	s2, err := sm.Create(ctx)
	require.NoError(t, err)

	notify := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  mcpwire.NotificationToolsListChanged,
	}
	sm.BroadcastNotification(notify)

	for _, s := range []*Session{s1, s2} {
		select {
		case raw := <-s.Out():
			var got rpc.Request
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, mcpwire.NotificationToolsListChanged, got.Method)
			require.True(t, got.IsNotification())
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for notification on session %s", s.ID())
		}
	}
}

func TestEnqueueNotificationOmitsID(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	s := NewSession(context.Background(), "s", mpx, nil)
	require.NoError(t, s.EnqueueNotification(&rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  mcpwire.LegacyToolsListChanged,
	}))

	select {
	case raw := <-s.Out():
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &m))
		_, hasID := m["id"]
		require.False(t, hasID)
		require.Equal(t, json.RawMessage(`"`+mcpwire.LegacyToolsListChanged+`"`), m["method"])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

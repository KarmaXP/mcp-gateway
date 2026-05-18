package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
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
	require.True(t, errors.Is(err, errOutboundBufferFull))
	require.Equal(t, uint64(1), s.DroppedOutbound())
}

func TestDispatchEmitsJSONRPCErrorWhenOutboundFull(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	mpx, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "full-dispatch", mpx, nil)
	handshake(t, s)
	for {
		select {
		case <-s.Out():
		default:
			goto filled
		}
	}
filled:
	for i := 0; i < defaults.SessionOutboundChannelSize; i++ {
		s.out <- []byte("blocked")
	}

	err = s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`99`),
	})
	require.NoError(t, err)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-s.Out():
			var resp rpc.Response
			if json.Unmarshal(raw, &resp) != nil || resp.Error == nil {
				continue
			}
			var id int
			if json.Unmarshal(resp.ID, &id) != nil || id != 99 {
				continue
			}
			require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
			require.Contains(t, resp.Error.Message, "session outbound buffer full")
			return
		case <-deadline:
			t.Fatal("timeout waiting for JSON-RPC backpressure error on SSE")
		}
	}
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

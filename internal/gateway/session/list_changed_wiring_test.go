package session

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type listChangedWiringUpstream struct {
	backend.Upstream
	onNotify func(*rpc.Request)
}

func (u *listChangedWiringUpstream) SetOnNotification(fn func(*rpc.Request)) {
	u.onNotify = fn
}

func (u *listChangedWiringUpstream) deliverNotification(req *rpc.Request) {
	if u.onNotify != nil {
		u.onNotify(req)
	}
}

type countingToolsListBackend struct {
	inner backend.Upstream
	calls atomic.Int32
}

func (c *countingToolsListBackend) ID() string     { return c.inner.ID() }
func (c *countingToolsListBackend) Prefix() string { return c.inner.Prefix() }

func (c *countingToolsListBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		c.calls.Add(1)
	}
	return c.inner.Call(ctx, req)
}

func TestListChangedHandlerWiringInvalidatesCacheAndBroadcasts(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	counter := &countingToolsListBackend{inner: inner}
	up := &listChangedWiringUpstream{Upstream: counter}

	mpx, err := multiplex.New([]backend.Upstream{up}, multiplex.WithListTTL(time.Minute))
	require.NoError(t, err)

	sm := NewSessionManager(mpx)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := sm.Create(ctx)

	backend.RegisterNotificationHandlers([]backend.Upstream{up}, func(req *rpc.Request) {
		if req == nil || !mcpwire.IsToolsListChangedNotification(req.Method) {
			return
		}
		mpx.InvalidateToolCache()
		sm.BroadcastNotification(req)
	})

	rpcCtx := context.Background()
	id := json.RawMessage(`1`)
	_, err = mpx.ToolsList(rpcCtx, id)
	require.NoError(t, err)
	_, err = mpx.ToolsList(rpcCtx, id)
	require.NoError(t, err)
	require.Equal(t, int32(1), counter.calls.Load())

	up.deliverNotification(&rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  mcpwire.NotificationToolsListChanged,
	})

	select {
	case raw := <-sess.Out():
		var got rpc.Request
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, mcpwire.NotificationToolsListChanged, got.Method)
		require.True(t, got.IsNotification())
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for list_changed on host session")
	}

	_, err = mpx.ToolsList(rpcCtx, id)
	require.NoError(t, err)
	require.Equal(t, int32(2), counter.calls.Load())
}

package session

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type listChangedWiringUpstream struct {
	upstream.Client
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

type countingToolsListUpstream struct {
	inner upstream.Client
	calls atomic.Int32
}

func (c *countingToolsListUpstream) ID() string     { return c.inner.ID() }
func (c *countingToolsListUpstream) Prefix() string { return c.inner.Prefix() }

func (c *countingToolsListUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		c.calls.Add(1)
	}
	return c.inner.Call(ctx, req)
}

func TestListChangedHandlerWiringInvalidatesCacheAndBroadcasts(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	counter := &countingToolsListUpstream{inner: inner}
	up := &listChangedWiringUpstream{Client: counter}

	mpx, err := multiplex.New(context.Background(), []upstream.Client{up}, multiplex.WithListTTL(time.Minute))
	require.NoError(t, err)

	sm := NewSessionManager(context.Background(), mpx)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess, err := sm.Create(ctx)
	require.NoError(t, err)

	upstream.RegisterNotificationHandlers([]upstream.Client{up}, func(req *rpc.Request) {
		if req == nil || !mcpwire.IsCatalogListChangedNotification(req.Method) {
			return
		}
		if mcpwire.IsToolsListChangedNotification(req.Method) {
			mpx.HandleToolsListChanged()
		}
		sm.BroadcastNotification(req)
	})

	rpcCtx := context.Background()
	id := json.RawMessage(`1`)
	_, err = mpx.ToolsList(rpcCtx, id)
	require.NoError(t, err)
	_, err = mpx.ToolsList(rpcCtx, id)
	require.NoError(t, err)
	require.Equal(t, int32(1), counter.calls.Load())

	nonToolMethods := []string{
		mcpwire.NotificationResourcesListChanged,
		mcpwire.LegacyResourcesListChanged,
		mcpwire.NotificationPromptsListChanged,
		mcpwire.LegacyPromptsListChanged,
	}
	for _, method := range nonToolMethods {
		method := method
		t.Run("broadcast-"+method, func(t *testing.T) {
			up.deliverNotification(&rpc.Request{
				JSONRPC: rpc.JSONRPCVersion,
				Method:  method,
			})

			select {
			case raw := <-sess.Out():
				var got rpc.Request
				require.NoError(t, json.Unmarshal(raw, &got))
				require.Equal(t, method, got.Method)
				require.True(t, got.IsNotification())
			case <-time.After(2 * time.Second):
				t.Fatalf("timeout waiting for %s on host session", method)
			}

			_, err = mpx.ToolsList(rpcCtx, id)
			require.NoError(t, err)
			require.Equal(t, int32(1), counter.calls.Load(), "%s must not invalidate tools cache", method)
		})
	}

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

	up.deliverNotification(&rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  mcpwire.LegacyToolsListChanged,
	})

	select {
	case raw := <-sess.Out():
		var got rpc.Request
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, mcpwire.LegacyToolsListChanged, got.Method)
		require.True(t, got.IsNotification())
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for legacy list_changed on host session")
	}

	_, err = mpx.ToolsList(rpcCtx, id)
	require.NoError(t, err)
	require.Equal(t, int32(3), counter.calls.Load())
}

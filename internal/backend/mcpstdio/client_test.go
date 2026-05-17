package mcpstdio

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestNewStdioMCPUpstream_EmptyCommand(t *testing.T) {
	t.Parallel()
	_, _, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", nil, nil, 1)
	require.Error(t, err)
}

func TestDispatchNotificationInvokesCallback(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	var (
		mu   sync.Mutex
		seen *rpc.Request
	)
	c.SetOnNotification(func(req *rpc.Request) {
		mu.Lock()
		seen = req
		mu.Unlock()
	})

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"method":  mcpwire.NotificationToolsListChanged,
	})
	require.NoError(t, err)
	c.dispatch(raw)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, seen)
	require.Equal(t, mcpwire.NotificationToolsListChanged, seen.Method)
}

func TestDispatchResponseDeliversToPending(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	ch := make(chan *rpc.Response, 1)
	c.pendMu.Lock()
	c.pending["1"] = ch
	c.pendMu.Unlock()

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      1,
		"result":  map[string]any{"ok": true},
	})
	require.NoError(t, err)
	c.dispatch(raw)

	select {
	case resp := <-ch:
		require.NotNil(t, resp)
		require.Nil(t, resp.Error)
	default:
		t.Fatal("expected response on pending channel")
	}
}

func TestDispatchResponseIgnoresUnknownID(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      99,
		"result":  map[string]any{},
	})
	require.NoError(t, err)
	c.dispatch(raw)
}

func TestIDKey(t *testing.T) {
	t.Parallel()
	require.Empty(t, idKey(nil))
	require.Equal(t, "42", idKey(json.RawMessage(`42`)))
}

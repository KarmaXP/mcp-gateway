package mcphttp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestDispatchNotificationInvokesCallback(t *testing.T) {
	t.Parallel()
	methods := []string{
		mcpwire.NotificationToolsListChanged,
		mcpwire.LegacyToolsListChanged,
		mcpwire.NotificationResourcesListChanged,
		mcpwire.LegacyResourcesListChanged,
		mcpwire.NotificationPromptsListChanged,
		mcpwire.LegacyPromptsListChanged,
	}
	for _, method := range methods {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", "http://example.invalid", 1, "")
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
				"method":  method,
			})
			require.NoError(t, err)
			c.dispatch(raw)

			mu.Lock()
			defer mu.Unlock()
			require.NotNil(t, seen)
			require.Equal(t, method, seen.Method)
			require.True(t, seen.IsNotification())
		})
	}
}

func TestDispatchResponseDoesNotInvokeCallback(t *testing.T) {
	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", "http://example.invalid", 1, "")
	require.NoError(t, err)
	defer cleanup()

	called := false
	c.SetOnNotification(func(req *rpc.Request) {
		called = true
	})

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      1,
		"result":  map[string]any{},
	})
	require.NoError(t, err)
	c.dispatch(raw)
	require.False(t, called)
}

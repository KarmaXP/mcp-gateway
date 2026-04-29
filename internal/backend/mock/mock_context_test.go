package mock

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestBackendToolsCallRespectsContextCancelWithDelay(t *testing.T) {
	b := NewMockUpstream("b1", "p", []string{"echo"})
	b.ToolsCallDelay = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	params, _ := json.Marshal(map[string]any{"name": "echo", "arguments": map[string]any{}})
	req := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`1`),
		Params:  params,
	}
	_, err := b.Call(ctx, req)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

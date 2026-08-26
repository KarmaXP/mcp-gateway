package mock

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestToolsCallErrPropagates(t *testing.T) {
	b := NewMockUpstream("b1", "p", []string{"echo"})
	b.ToolsCallErr = errors.New("upstream")
	params, _ := json.Marshal(map[string]any{"name": "echo"})
	_, err := b.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`1`),
		Params:  params,
	})
	require.Error(t, err)
}

func TestToolsCallUnknownTool(t *testing.T) {
	b := NewMockUpstream("b1", "p", []string{"echo"})
	params, _ := json.Marshal(map[string]any{"name": "nope"})
	resp, err := b.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`1`),
		Params:  params,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
}

func TestToolsCallInvalidParamsJSON(t *testing.T) {
	b := NewMockUpstream("b1", "p", []string{"echo"})
	resp, err := b.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`not-json`),
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
}

func TestUnknownMethod(t *testing.T) {
	b := NewMockUpstream("b1", "p", []string{"echo"})
	resp, err := b.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "custom/xyz",
		ID:      json.RawMessage(`1`),
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
}

func TestInitializeAndToolsList(t *testing.T) {
	b := NewMockUpstream("b1", "p", []string{"echo", "list"})
	initResp, err := b.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
	})
	require.NoError(t, err)
	require.Nil(t, initResp.Error)
	listResp, err := b.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`2`),
	})
	require.NoError(t, err)
	require.Nil(t, listResp.Error)
	require.Contains(t, string(listResp.Result), "echo")
}

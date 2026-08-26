package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type nilInitUpstream struct{ inner upstream.Client }

func (n *nilInitUpstream) ID() string     { return n.inner.ID() }
func (n *nilInitUpstream) Prefix() string { return n.inner.Prefix() }
func (n *nilInitUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "initialize" {
		return nil, nil
	}
	return n.inner.Call(ctx, req)
}

type nilCallUpstream struct{ inner upstream.Client }

func (n *nilCallUpstream) ID() string     { return n.inner.ID() }
func (n *nilCallUpstream) Prefix() string { return n.inner.Prefix() }
func (n *nilCallUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/call" {
		return nil, nil
	}
	return n.inner.Call(ctx, req)
}

func TestSessionDispatchInitializeNilMuxResponseNoPanic(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := multiplex.New(context.Background(), []upstream.Client{&nilInitUpstream{inner: inner}}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "init-nil", mpx, nil)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))

	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

func TestSessionDispatchToolsCallNilMuxResponseNoPanic(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := multiplex.New(context.Background(), []upstream.Client{&nilCallUpstream{inner: inner}}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "call-nil", mpx, nil)
	handshake(t, s)

	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`2`),
		Params:  params,
	}))

	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

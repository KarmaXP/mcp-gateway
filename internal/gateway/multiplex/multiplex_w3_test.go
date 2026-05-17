package multiplex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestToolsCallRejectsEmptyToolName(t *testing.T) {
	b := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := New([]backend.Upstream{b}, WithListTTL(0))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage(`0`))
	require.NoError(t, err)

	params, _ := json.Marshal(map[string]any{"name": "  ", "arguments": map[string]any{}})
	resp, err := mpx.ToolsCall(context.Background(), json.RawMessage(`1`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
}

func TestMergeNamespacedToolListDeduplicatesByName(t *testing.T) {
	alpha := mock.NewMockUpstream("a", "alpha", []string{"echo"})
	per := [][]map[string]any{
		{{"name": "echo"}, {"name": "echo"}},
	}
	merged := mergeNamespacedToolList([]backend.Upstream{alpha}, per)
	require.Len(t, merged, 1)
	require.Equal(t, "alpha__echo", merged[0]["name"])
}

type nilListResponseBackend struct {
	*mock.MockUpstream
}

func (n *nilListResponseBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	switch req.Method {
	case "resources/list", "prompts/list", "resources/read", "prompts/get":
		return nil, nil
	default:
		return n.MockUpstream.Call(ctx, req)
	}
}

func TestResourcesListNilUpstreamResponseNoPanic(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", nil)
	up := &nilListResponseBackend{MockUpstream: inner}
	mpx, err := New([]backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage(`0`))
	require.NoError(t, err)

	resp, err := mpx.ResourcesList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
}

func TestInvokeUpstreamGenericNilResponse(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	up := &nilListResponseBackend{MockUpstream: inner}
	mpx, err := New([]backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage(`0`))
	require.NoError(t, err)

	params, _ := json.Marshal(map[string]any{"uri": "alpha__file:///x/y"})
	resp, err := mpx.ResourcesRead(context.Background(), json.RawMessage(`2`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

func TestHandleToolsListChangedRespectsCanceledLifecycleContext(t *testing.T) {
	b := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	lifecycle, cancel := context.WithCancel(context.Background())
	cancel()
	mpx, err := New([]backend.Upstream{b}, WithListTTL(0), WithLifecycleContext(lifecycle), WithListTimeout(2*time.Second))
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		mpx.HandleToolsListChanged(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleToolsListChanged blocked after lifecycle cancel")
	}
}

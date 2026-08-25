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
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestToolsCallRejectsEmptyToolName(t *testing.T) {
	b := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := New(context.Background(), []backend.Upstream{b}, WithListTTL(0))
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
	mpx, err := New(context.Background(), []backend.Upstream{up}, WithListTTL(0))
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
	mpx, err := New(context.Background(), []backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage(`0`))
	require.NoError(t, err)

	params, _ := json.Marshal(map[string]any{"uri": "alpha__file:///x/y"})
	resp, err := mpx.ResourcesRead(context.Background(), json.RawMessage(`2`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

type contextAwareUpstream struct {
	inner *dynamicToolsUpstream
}

func (u *contextAwareUpstream) ID() string     { return u.inner.ID() }
func (u *contextAwareUpstream) Prefix() string { return u.inner.Prefix() }

func (u *contextAwareUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return u.inner.Call(ctx, req)
}

func TestHandleToolsListChangedRespectsCanceledLifecycleContext(t *testing.T) {
	up := &contextAwareUpstream{inner: newDynamicToolsUpstream("b1", "alpha", []string{"echo"})}
	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = mode.AssistList
	sr := router.NewSemanticRouter(rcfg, &countingEmbed{dim: 4}, store.NewInMemoryVectorStore(4), 4)

	lifecycle, cancel := context.WithCancel(context.Background())
	cancel()
	mpx, err := New(lifecycle, []backend.Upstream{up},
		WithListTTL(0),
		WithSemanticRouter(sr),
		withToolsListChangedDebounce(0),
		WithListTimeout(2*time.Second),
	)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		mpx.HandleToolsListChanged()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleToolsListChanged blocked after lifecycle cancel")
	}
	require.Zero(t, up.inner.calls.Load(),
		"the refresh runs on the lifecycle context, so a canceled one must stop it before it reaches an upstream")
}

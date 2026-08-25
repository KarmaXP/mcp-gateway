package multiplex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type countingListBackend struct {
	inner backend.Upstream
	calls atomic.Int32
}

func (c *countingListBackend) ID() string     { return c.inner.ID() }
func (c *countingListBackend) Prefix() string { return c.inner.Prefix() }
func (c *countingListBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		c.calls.Add(1)
	}
	return c.inner.Call(ctx, req)
}

func TestToolsListCacheHitWhenTTLPositiveAndNoAllowList(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b := &countingListBackend{inner: inner}
	a, err := New(context.Background(), []backend.Upstream{b}, WithListTTL(time.Minute))
	require.NoError(t, err)

	ctx := context.Background()
	id := json.RawMessage(`1`)

	resp1, err := a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Nil(t, resp1.Error)
	require.Equal(t, int32(1), b.calls.Load())

	resp2, err := a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Nil(t, resp2.Error)
	require.Equal(t, int32(1), b.calls.Load(), "second tools/list should hit cache")
	require.JSONEq(t, string(resp1.Result), string(resp2.Result))
}

func TestToolsListCacheBypassedWithJWTAllowList(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b := &countingListBackend{inner: inner}
	a, err := New(context.Background(), []backend.Upstream{b}, WithListTTL(time.Minute))
	require.NoError(t, err)

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__echo"})
	id := json.RawMessage(`1`)

	resp1, err := a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Nil(t, resp1.Error)
	require.Equal(t, int32(1), b.calls.Load())

	resp2, err := a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Nil(t, resp2.Error)
	require.Equal(t, int32(2), b.calls.Load(), "JWT allow-list must bypass tools/list cache")
}

func TestToolsListCacheInvalidatedAfterListChanged(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b := &countingListBackend{inner: inner}
	a, err := New(context.Background(), []backend.Upstream{b}, WithListTTL(time.Minute))
	require.NoError(t, err)

	ctx := context.Background()
	id := json.RawMessage(`1`)

	_, err = a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int32(1), b.calls.Load())

	_, err = a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int32(1), b.calls.Load())

	a.InvalidateToolCache()

	_, err = a.ToolsList(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int32(2), b.calls.Load(), "InvalidateToolCache must force upstream tools/list")
}

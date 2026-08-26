package multiplex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type countingListUpstream struct {
	inner upstream.Client
	calls atomic.Int32
}

func (c *countingListUpstream) ID() string     { return c.inner.ID() }
func (c *countingListUpstream) Prefix() string { return c.inner.Prefix() }
func (c *countingListUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		c.calls.Add(1)
	}
	return c.inner.Call(ctx, req)
}

func TestToolsListCacheHitWhenTTLPositiveAndNoAllowList(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b := &countingListUpstream{inner: inner}
	a, err := New(context.Background(), []upstream.Client{b}, WithListTTL(time.Minute))
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
	b := &countingListUpstream{inner: inner}
	a, err := New(context.Background(), []upstream.Client{b}, WithListTTL(time.Minute))
	require.NoError(t, err)

	ctx := hostctx.WithAllowList(context.Background(), []string{"alpha__echo"})
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
	b := &countingListUpstream{inner: inner}
	a, err := New(context.Background(), []upstream.Client{b}, WithListTTL(time.Minute))
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

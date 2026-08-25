package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type failingToolsListUpstream struct {
	id     string
	prefix string
	delay  time.Duration
	calls  atomic.Int32
}

func (u *failingToolsListUpstream) ID() string     { return u.id }
func (u *failingToolsListUpstream) Prefix() string { return u.prefix }

func (u *failingToolsListUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	switch req.Method {
	case "tools/list":
		u.calls.Add(1)
		if u.delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(u.delay):
			}
		}
		return nil, errors.New("boom")
	default:
		return rpc.NewError(req.ID, errcodes.MethodNotFound, "method not found", nil), nil
	}
}

func TestHandleToolsListChangedWithoutSemanticInvalidatesCache(t *testing.T) {
	up := newDynamicToolsUpstream("b1", "p", []string{"echo"})
	a, err := New(context.Background(), []backend.Upstream{up}, WithListTTL(time.Minute))
	require.NoError(t, err)

	_, err = a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	_, err = a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Equal(t, int32(1), up.calls.Load(), "second tools/list should hit cache")

	require.NotPanics(t, func() {
		a.HandleToolsListChanged()
	})

	_, err = a.ToolsList(context.Background(), json.RawMessage(`3`))
	require.NoError(t, err)
	require.Equal(t, int32(2), up.calls.Load(), "list_changed should invalidate tools cache when semantic router is nil")
}

func TestHandleToolsListChangedDebouncesUpstreamRefresh(t *testing.T) {
	up := newDynamicToolsUpstream("b1", "p", []string{"echo"})
	emb := &countingEmbed{dim: 4}
	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = mode.AssistList
	rcfg.ScoreMin = 0.01
	rcfg.TopK = 8
	sr := router.NewSemanticRouter(rcfg, emb, store.NewInMemoryVectorStore(4), 4)

	a, err := New(
		context.Background(),
		[]backend.Upstream{up},
		WithListTTL(time.Minute),
		WithSemanticRouter(sr),
		WithToolsListChangedDebounce(80*time.Millisecond),
	)
	require.NoError(t, err)

	_, err = a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Equal(t, int32(1), up.calls.Load())

	for range 5 {
		a.HandleToolsListChanged()
	}
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, int32(2), up.calls.Load(), "bursts of list_changed should coalesce to one upstream tools/list refresh")
}

func TestHandleToolsListChangedTimesOutOnStrictUpstreamFailure(t *testing.T) {
	good := newDynamicToolsUpstream("b1", "p", []string{"echo"})
	bad := &failingToolsListUpstream{
		id:     "b2",
		prefix: "q",
		delay:  100 * time.Millisecond,
	}
	emb := &mapEmbed{dim: 4, vecs: map[string][]float32{}}
	sr, _ := routerTestSemanticRouter(t, emb, 0.99, true)

	a, err := New(
		context.Background(),
		[]backend.Upstream{good, bad},
		WithSemanticRouter(sr),
		WithListTimeout(20*time.Millisecond),
		WithAggregationStrict(false, true),
		WithToolsListChangedDebounce(0),
	)
	require.NoError(t, err)

	start := time.Now()
	require.NotPanics(t, func() {
		a.HandleToolsListChanged()
	})
	elapsed := time.Since(start)
	require.Less(t, elapsed, 80*time.Millisecond, "list_changed handler should return quickly when debounce is disabled")
	require.Equal(t, int32(1), good.calls.Load(), "healthy upstream is still queried")
	require.Equal(t, int32(1), bad.calls.Load(), "failing upstream should be queried once")
}

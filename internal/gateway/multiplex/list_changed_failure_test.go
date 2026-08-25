package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type failableUpstream struct {
	inner   *dynamicToolsUpstream
	failing atomic.Bool
}

func (u *failableUpstream) ID() string     { return u.inner.ID() }
func (u *failableUpstream) Prefix() string { return u.inner.Prefix() }

func (u *failableUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if u.failing.Load() {
		return nil, errors.New("upstream unreachable")
	}
	return u.inner.Call(ctx, req)
}

func newListChangedMultiplexer(t *testing.T, ups ...backend.Upstream) *Multiplexer {
	t.Helper()
	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = mode.AssistList
	sr := router.NewSemanticRouter(rcfg, &countingEmbed{dim: 4}, store.NewInMemoryVectorStore(4), 4)
	a, err := New(context.Background(), ups,
		WithListTTL(0),
		WithSemanticRouter(sr),
		WithToolsListChangedDebounce(0),
	)
	require.NoError(t, err)
	return a
}

func TestListChangedRefreshKeepsCatalogWhenEveryUpstreamFails(t *testing.T) {
	up := &failableUpstream{inner: newDynamicToolsUpstream("b1", "alpha", []string{"echo"})}
	a := newListChangedMultiplexer(t, up)

	_, err := a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, a.schemaRegistry.lookup("alpha__echo").validator)
	indexedVersion := a.catalogVersion.load()
	require.NotEmpty(t, indexedVersion)

	up.failing.Store(true)
	a.HandleToolsListChanged()

	require.NotNil(t, a.schemaRegistry.lookup("alpha__echo").validator,
		"a refresh where every upstream failed must not disarm argument validation")
	require.Equal(t, indexedVersion, a.catalogVersion.load(),
		"a refresh where every upstream failed must not reindex the catalog to empty")
}

func TestListChangedRefreshStillAppliesWhenOnlySomeUpstreamsFail(t *testing.T) {
	healthy := &failableUpstream{inner: newDynamicToolsUpstream("b1", "alpha", []string{"echo"})}
	broken := &failableUpstream{inner: newDynamicToolsUpstream("b2", "beta", []string{"ping"})}
	a := newListChangedMultiplexer(t, healthy, broken)

	_, err := a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, a.schemaRegistry.lookup("beta__ping").validator)

	healthy.inner.SetTools([]string{"echo", "added"})
	broken.failing.Store(true)
	a.HandleToolsListChanged()

	require.NotNil(t, a.schemaRegistry.lookup("alpha__added").validator,
		"one failing sibling must not stop the refresh from picking up a reachable upstream's new tool")
}

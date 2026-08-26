package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestInvalidateToolCachePreservesCatalogVersion(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	emb := &countingEmbed{dim: 4}
	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = mode.AssistList
	sr := router.NewSemanticRouter(rcfg, emb, store.NewInMemoryVectorStore(4), 4)

	a, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), WithSemanticRouter(sr))
	require.NoError(t, err)

	_, err = a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	ver := a.catalogVersion.load()
	require.NotEmpty(t, ver)
	require.Equal(t, ver, sr.CatalogVersion())

	a.InvalidateToolCache()

	require.Equal(t, ver, a.catalogVersion.load(), "invalidate must keep catalog version until reindex succeeds")
}

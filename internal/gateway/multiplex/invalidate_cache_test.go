package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func TestInvalidateToolCachePreservesCatalogVersion(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	emb := &countingEmbed{dim: 4}
	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = router.ModeAssistList
	sr := router.NewSemanticRouter(rcfg, emb, store.NewInMemoryVectorStore(4), 4)

	a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithSemanticRouter(sr))
	require.NoError(t, err)

	_, err = a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	a.catMu.RLock()
	ver := a.catVer
	a.catMu.RUnlock()
	require.NotEmpty(t, ver)
	require.Equal(t, ver, sr.CatalogVersion())

	a.InvalidateToolCache()

	a.catMu.RLock()
	defer a.catMu.RUnlock()
	require.Equal(t, ver, a.catVer, "invalidate must keep catalog version until reindex succeeds")
}

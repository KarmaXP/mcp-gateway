package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestSemanticRoutingSignalIncludesIntentAndCatalogVersion(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	a, err := New(context.Background(), []upstream.Client{b1})
	require.NoError(t, err)
	a.catalogVersion.version = "catalog-ver-xyz"

	ctx := hostctx.WithClientIntent(context.Background(), "operator wants pod logs")
	ctx = hostctx.WithAllowList(ctx, []string{"p__echo", "other__tool"})
	sig := a.semanticRoutingSignal(ctx, "p__echo", json.RawMessage(`{"x":1}`))
	require.Equal(t, "p__echo", sig.ToolName)
	require.JSONEq(t, `{"x":1}`, string(sig.ArgumentsJSON))
	require.Equal(t, "operator wants pod logs", sig.IntentText)
	require.Equal(t, []string{"p__echo", "other__tool"}, sig.AllowList)
	require.Equal(t, router.AllowListAuthzRestricted, sig.AllowListAuthz)
	require.Equal(t, "catalog-ver-xyz", sig.CatalogVersion)
}

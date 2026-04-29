package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
)

func TestSemanticRoutingSignalIncludesIntentAndCatalogVersion(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	a, err := New([]backend.Upstream{b1})
	require.NoError(t, err)
	a.catMu.Lock()
	a.catVer = "catalog-ver-xyz"
	a.catMu.Unlock()

	ctx := hostctx.WithClientIntent(context.Background(), "operator wants pod logs")
	ctx = hostctx.WithAllowedToolNames(ctx, []string{"p__echo", "other__tool"})
	sig := a.semanticRoutingSignal(ctx, "p__echo", json.RawMessage(`{"x":1}`))
	require.Equal(t, "tools/call", sig.Method)
	require.Equal(t, "p__echo", sig.ToolName)
	require.JSONEq(t, `{"x":1}`, string(sig.ArgumentsJSON))
	require.Equal(t, "operator wants pod logs", sig.IntentText)
	require.Equal(t, []string{"p__echo", "other__tool"}, sig.AllowedTools)
	require.Equal(t, "catalog-ver-xyz", sig.CatalogVersion)
}

package multiplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
)

func TestUnknownAllowListModeListsNoTools(t *testing.T) {
	merged := []map[string]any{
		{"name": "alpha__echo"},
		{"name": "alpha__secret"},
	}

	filtered, err := filterToolsForPolicy(merged, hostctx.AllowListMode(99), []string{"alpha__echo"})

	require.NoError(t, err)
	require.Empty(t, filtered,
		"a mode this function does not know must show nothing, or adding one widens the catalog silently")
}

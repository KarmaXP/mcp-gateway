package policy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandAuthorizationDetails_PatternAndName(t *testing.T) {
	raw, err := json.Marshal([]map[string]any{
		{"type": "mcp_tool", "tool_name": "a__one"},
		{"type": "mcp_tool", "tool_pattern": "b__*"},
	})
	require.NoError(t, err)
	got, err := expandAuthorizationDetails(raw)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a__one", "b__*"}, got)
}

func TestMatchTool_Glob(t *testing.T) {
	ok, err := MatchTool("prom__query_range", "prom__*")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = MatchTool("other__x", "prom__*")
	require.NoError(t, err)
	require.False(t, ok)
}

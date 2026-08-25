package policy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandAuthorizationDetails(t *testing.T) {
	tests := []struct {
		name      string
		raw       []map[string]any
		want      []string
		wantErr   string
		wantEmpty bool
	}{
		{
			name: "accepts exact names and globs",
			raw: []map[string]any{
				{"type": "mcp_tool", "tool_name": "a__one"},
				{"type": "mcp_tool", "tool_pattern": "b__*"},
			},
			want: []string{"a__one", "b__*"},
		},
		{
			name: "ignores non mcp tool types",
			raw: []map[string]any{
				{"type": "other_type", "tool_name": "ignored__tool"},
				{"type": "mcp_tool", "tool_name": "kept__tool"},
			},
			want: []string{"kept__tool"},
		},
		{
			name: "rejects mutually exclusive name and pattern",
			raw: []map[string]any{
				{"type": "mcp_tool", "tool_name": "a__one", "tool_pattern": "a__*"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "rejects mcp_tool without name or pattern",
			raw: []map[string]any{
				{"type": "mcp_tool"},
			},
			wantErr: "requires tool_name or tool_pattern",
		},
		{
			name: "trims and dedupes entries",
			raw: []map[string]any{
				{"type": "mcp_tool", "tool_name": "  prom__query_range  "},
				{"type": "mcp_tool", "tool_name": "prom__query_range"},
				{"type": "mcp_tool", "tool_pattern": "  k8s__*  "},
				{"type": "mcp_tool", "tool_pattern": "k8s__*"},
			},
			want: []string{"prom__query_range", "k8s__*"},
		},
		{
			name:      "returns empty when no mcp entries",
			raw:       []map[string]any{{"type": "non_tool", "tool_name": "x"}},
			wantEmpty: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.raw)
			require.NoError(t, err)

			got, err := expandAuthorizationDetails(raw)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			if tc.wantEmpty {
				require.Empty(t, got)
				return
			}
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestMatchTool(t *testing.T) {
	tests := []struct {
		name           string
		namespacedTool string
		entry          string
		wantMatch      bool
		wantErr        string
	}{
		{
			name:           "exact match without glob",
			namespacedTool: "prom__query_range",
			entry:          "prom__query_range",
			wantMatch:      true,
		},
		{
			name:           "glob star matches prefix",
			namespacedTool: "prom__query_range",
			entry:          "prom__*",
			wantMatch:      true,
		},
		{
			name:           "glob question mark edge case",
			namespacedTool: "k8s__pod1",
			entry:          "k8s__pod?",
			wantMatch:      true,
		},
		{
			name:           "character class is rejected, not interpreted",
			namespacedTool: "k8s__pod2",
			entry:          "k8s__pod[12]",
			wantErr:        "use only * and ?",
		},
		{
			name:           "unbalanced bracket is rejected",
			namespacedTool: "k8s__pod1",
			entry:          "k8s__pod[",
			wantErr:        "use only * and ?",
		},
		{
			name:           "empty tool returns false",
			namespacedTool: "",
			entry:          "k8s__*",
			wantMatch:      false,
		},
		{
			name:           "empty entry returns false",
			namespacedTool: "k8s__pod1",
			entry:          "",
			wantMatch:      false,
		},
		{
			name:           "non matching glob returns false",
			namespacedTool: "other__x",
			entry:          "prom__*",
			wantMatch:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := matchTool(tc.namespacedTool, tc.entry)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.True(t, strings.Contains(err.Error(), tc.wantErr))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantMatch, ok)
		})
	}
}

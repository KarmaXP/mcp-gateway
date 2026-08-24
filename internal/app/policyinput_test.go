package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/config"
)

func TestPolicyEngineInputRejectsUnsupportedToolPatterns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		settings  config.PolicySettings
		wantField string
	}{
		{
			name:      "elevated tool with a character class",
			settings:  config.PolicySettings{ElevatedTools: []string{"k8s__[prod]*"}},
			wantField: "policy.elevated_tools",
		},
		{
			name:      "tool group with an escape",
			settings:  config.PolicySettings{ToolGroups: map[string][]string{"sre": {`k8s__\*`}}},
			wantField: "policy.tool_groups[sre]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := policyEngineInput(config.GatewayConfig{Policy: tc.settings})
			require.Error(t, err, "an unmatchable pattern must stop startup, not silently protect nothing")
			require.ErrorContains(t, err, tc.wantField)
		})
	}
}

func TestPolicyEngineInputAcceptsSupportedPatterns(t *testing.T) {
	t.Parallel()
	_, err := policyEngineInput(config.GatewayConfig{Policy: config.PolicySettings{
		ElevatedTools: []string{"k8s__*", "gh__get_pr", "prom__quer?"},
		ToolGroups:    map[string][]string{"sre": {"k8s__*"}},
	}})
	require.NoError(t, err, "* and ? are the supported syntax and must not be rejected")
}

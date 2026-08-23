package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func writeConfig(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gateway.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	t.Setenv("MCP_GATEWAY_CONFIG", p)
}

const oneUpstream = `
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
`

func TestLoadRejectsUnknownYAMLKeys(t *testing.T) {
	tests := []struct{ name, key string }{
		{name: "misspelled section", key: "aggregaton:\n  strict_list: true"},
		{name: "misspelled scalar", key: "maxconcurrency: 5"},
		{name: "misspelled nested key", key: "aggregation:\n  strict_lst: true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeConfig(t, oneUpstream+tc.key+"\n")
			_, err := Load()
			require.Error(t, err, "a misspelled key must not be silently ignored")
			require.Contains(t, err.Error(), "not found")
		})
	}
}

func TestLoadValidatesAfterEnvironmentOverrides(t *testing.T) {
	writeConfig(t, oneUpstream+"router:\n  mode: enabled\n")
	t.Setenv("ROUTER_MODE", "off")
	cfg, err := Load()
	require.NoError(t, err, "the environment overrides the file, so validation belongs after it")
	require.Equal(t, "off", cfg.SemanticRouter.Mode)
}

func TestEnvironmentBooleansTurnFlagsOffAsWellAsOn(t *testing.T) {
	writeConfig(t, oneUpstream+"aggregation:\n  strict_list: true\n")
	t.Setenv("AGGREGATION_STRICT_LIST", "false")
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.Aggregation.StrictList,
		"an operator must be able to disable strict aggregation during an incident")
}

func TestUnparseableEnvironmentBooleanLeavesTheFileValue(t *testing.T) {
	writeConfig(t, oneUpstream+"router:\n  mode: off\n  allow_auto_rename: true\n")
	t.Setenv("ROUTER_ALLOW_AUTO_RENAME", "flase")
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.SemanticRouter.AllowAutoRename,
		"a typo must not silently flip a flag to false")
}

func TestLoadRejectsMalformedDurations(t *testing.T) {
	tests := []struct{ name, body, wantField string }{
		{name: "router embed timeout", body: "router:\n  mode: off\n  embed_timeout: 10sec\n", wantField: "router.embed_timeout"},
		{name: "aggregation call timeout", body: "aggregation:\n  call_timeout: soon\n", wantField: "aggregation.call_timeout"},
		{name: "list cache ttl", body: "aggregation:\n  list_cache_ttl: 30\n", wantField: "aggregation.list_cache_ttl"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeConfig(t, oneUpstream+tc.body)
			_, err := Load()
			require.Error(t, err, "a malformed duration must fail startup, not fall back in silence")
			require.Contains(t, err.Error(), tc.wantField)
		})
	}
}

func TestLoadResolvesAbsentDurationsToTheirDefaults(t *testing.T) {
	writeConfig(t, oneUpstream)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, defaults.MultiplexInitTimeout, cfg.AggregationInitTimeout())
	require.Equal(t, defaults.MultiplexListCacheTTL, cfg.AggregationListCacheTTL())
	require.Equal(t, defaults.MultiplexCallTimeout.String(), cfg.Aggregation.CallTimeout,
		"normalize resolves the field itself, so the getter is no longer where the default lives")
}

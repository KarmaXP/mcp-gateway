package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestAggregationListCacheTTLUnsetUsesTheDefault(t *testing.T) {
	var c GatewayConfig
	require.Equal(t, defaults.MultiplexListCacheTTL, c.AggregationListCacheTTL(),
		"an absent field must fall back like its four siblings, or the default is dead code")
}

func TestAggregationListCacheTTLExplicitZeroDisablesTheCache(t *testing.T) {
	cfg := loadWithAggregation(t, "list_cache_ttl: 0s")
	require.Equal(t, time.Duration(0), cfg.AggregationListCacheTTL(),
		"disabling the cache needs an explicit sentinel, not an absent field")
}

func TestAggregationListCacheTTLParsePositive(t *testing.T) {
	cfg := loadWithAggregation(t, "list_cache_ttl: 30s")
	require.Equal(t, 30*time.Second, cfg.AggregationListCacheTTL())
}

func TestAggregationListCacheTTLLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
aggregation:
  list_cache_ttl: 45s
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 45*time.Second, cfg.AggregationListCacheTTL())
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAggregationListCacheTTLUnsetOrZero(t *testing.T) {
	var c GatewayConfig
	require.Equal(t, time.Duration(0), c.AggregationListCacheTTL())

	c = GatewayConfig{Aggregation: AggregationSettings{ListCacheTTL: "0s"}}
	require.Equal(t, time.Duration(0), c.AggregationListCacheTTL())
}

func TestAggregationListCacheTTLParsePositive(t *testing.T) {
	c := GatewayConfig{Aggregation: AggregationSettings{ListCacheTTL: "30s"}}
	require.Equal(t, 30*time.Second, c.AggregationListCacheTTL())
}

func TestAggregationListCacheTTLInvalidIgnored(t *testing.T) {
	c := GatewayConfig{Aggregation: AggregationSettings{ListCacheTTL: "not-a-duration"}}
	require.Equal(t, time.Duration(0), c.AggregationListCacheTTL())
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

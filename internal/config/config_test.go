package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestLoadYAMLAndEnvBackends(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
    max_concurrency: 2
`), 0o644)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 1)
	require.Equal(t, "one", cfg.Upstreams[0].ID)
	require.Equal(t, "a", cfg.Upstreams[0].Prefix)
	require.Equal(t, 2, cfg.Upstreams[0].MaxConcurrency)
}

func TestLoadBackendsJSONOnly(t *testing.T) {
	t.Setenv("MCP_GATEWAY_CONFIG", "")
	t.Chdir(t.TempDir())
	raw, _ := json.Marshal([]UpstreamDefinition{
		{ID: "x", Prefix: "p", URL: "http://localhost:1"},
	})
	t.Setenv("MCP_GATEWAY_BACKENDS", string(raw))
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 1)
	require.Equal(t, "x", cfg.Upstreams[0].ID)
}

func TestValidateDuplicatePrefix(t *testing.T) {
	cfg := GatewayConfig{Upstreams: []UpstreamDefinition{
		{ID: "a", Prefix: "p", URL: "http://a"},
		{ID: "b", Prefix: "p", URL: "http://b"},
	}}
	require.Error(t, cfg.Validate())
}

func TestQdrantCollectionDefault(t *testing.T) {
	var c GatewayConfig
	require.Equal(t, defaults.DefaultQdrantCollectionName, c.QdrantCollection())
	c.Qdrant.Collection = "custom"
	require.Equal(t, "custom", c.QdrantCollection())
}

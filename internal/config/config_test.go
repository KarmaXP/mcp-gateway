package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
	t.Setenv("MCP_GATEWAY_BACKENDS", "") // clear JSON merge for this test
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Backends, 1)
	require.Equal(t, "one", cfg.Backends[0].ID)
	require.Equal(t, "a", cfg.Backends[0].Prefix)
	require.Equal(t, 2, cfg.Backends[0].MaxConcurrency)
}

func TestLoadBackendsJSONOnly(t *testing.T) {
	t.Setenv("MCP_GATEWAY_CONFIG", "") // no file
	// prevent picking up gateway.yaml from repo if present
	t.Chdir(t.TempDir())
	raw, _ := json.Marshal([]Backend{
		{ID: "x", Prefix: "p", URL: "http://localhost:1"},
	})
	t.Setenv("MCP_GATEWAY_BACKENDS", string(raw))
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Backends, 1)
	require.Equal(t, "x", cfg.Backends[0].ID)
}

func TestValidateDuplicatePrefix(t *testing.T) {
	cfg := Config{Backends: []Backend{
		{ID: "a", Prefix: "p", URL: "http://a"},
		{ID: "b", Prefix: "p", URL: "http://b"},
	}}
	require.Error(t, cfg.Validate())
}

func TestQdrantCollectionDefault(t *testing.T) {
	var c Config
	require.Equal(t, "mcp_tool_catalog", c.QdrantCollection())
	c.Qdrant.Collection = "custom"
	require.Equal(t, "custom", c.QdrantCollection())
}

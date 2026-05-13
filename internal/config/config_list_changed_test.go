package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForwardToolsListChangedDefaultFalse(t *testing.T) {
	var c GatewayConfig
	require.False(t, c.ForwardToolsListChanged())
}

func TestForwardToolsListChangedFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
aggregation:
  forward_tools_list_changed: true
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("AGGREGATION_FORWARD_TOOLS_LIST_CHANGED", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.ForwardToolsListChanged())
}

func TestForwardToolsListChangedEnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("AGGREGATION_FORWARD_TOOLS_LIST_CHANGED", "true")
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.ForwardToolsListChanged())
}

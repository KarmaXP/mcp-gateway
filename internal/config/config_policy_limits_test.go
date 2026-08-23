package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyHardenSchemasEnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("POLICY_HARDEN_SCHEMAS", "true")

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.Policy.HardenSchemas)
}

func TestPolicyHardenSchemasEnvOverrideCanDisableYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
policy:
  harden_schemas: true
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("POLICY_HARDEN_SCHEMAS", "false")

	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.Policy.HardenSchemas)
}

func TestPolicyHardenSchemasEnvInvalidKeepsYAMLValue(t *testing.T) {
	cfg := GatewayConfig{
		Policy: PolicySettings{
			HardenSchemas: true,
		},
	}
	t.Setenv("POLICY_HARDEN_SCHEMAS", "not-a-bool")
	cfg.ApplyEnvOverrides()
	require.True(t, cfg.Policy.HardenSchemas)
}

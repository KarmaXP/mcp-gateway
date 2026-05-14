package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
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

func TestPolicyArgumentLimitsDefaults(t *testing.T) {
	var cfg GatewayConfig
	lim := cfg.PolicyArgumentLimits()
	require.Equal(t, defaults.MaxToolArgumentsJSONBytes, lim.MaxBytes)
	require.Equal(t, defaults.MaxToolArgumentsJSONDepth, lim.MaxDepth)
	require.Equal(t, defaults.MaxToolArgumentsJSONKeys, lim.MaxKeys)
}

func TestPolicyArgumentLimitsFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
policy:
  max_argument_bytes: 1234
  max_argument_depth: 12
  max_argument_keys: 77
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")

	cfg, err := Load()
	require.NoError(t, err)
	lim := cfg.PolicyArgumentLimits()
	require.Equal(t, 1234, lim.MaxBytes)
	require.Equal(t, 12, lim.MaxDepth)
	require.Equal(t, 77, lim.MaxKeys)
}

func TestPolicyArgumentLimitsZeroFallsBackToDefaults(t *testing.T) {
	cfg := GatewayConfig{
		Policy: PolicySettings{
			MaxArgumentBytes: 0,
			MaxArgumentDepth: -1,
			MaxArgumentKeys:  0,
		},
	}
	lim := cfg.PolicyArgumentLimits()
	require.Equal(t, defaults.MaxToolArgumentsJSONBytes, lim.MaxBytes)
	require.Equal(t, defaults.MaxToolArgumentsJSONDepth, lim.MaxDepth)
	require.Equal(t, defaults.MaxToolArgumentsJSONKeys, lim.MaxKeys)
}

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyHardenSchemasDefaultsToHardenedWhenAbsent(t *testing.T) {
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

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.PolicyHardenSchemas(), "additionalProperties defaults to false (SEC4)")
}

func TestPolicyHardenSchemasEnvOverrideCanReenableDisabledYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
policy:
  harden_schemas: false
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("POLICY_HARDEN_SCHEMAS", "true")

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.PolicyHardenSchemas())
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
	require.False(t, cfg.PolicyHardenSchemas())
}

func TestPolicyHardenSchemasEnvInvalidKeepsYAMLValue(t *testing.T) {
	disabled := false
	cfg := GatewayConfig{Policy: PolicySettings{HardenSchemas: &disabled}}
	t.Setenv("POLICY_HARDEN_SCHEMAS", "not-a-bool")
	cfg.ApplyEnvOverrides()
	require.False(t, cfg.PolicyHardenSchemas(), "an unparseable value must not flip the configured one")
}

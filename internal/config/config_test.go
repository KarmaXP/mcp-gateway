package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestLoadPolicyBlockFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
policy:
  version: "test-pol"
  harden_schemas: true
  max_argument_bytes: 4096
  max_argument_depth: 10
  max_argument_keys: 99
  elevated_tools:
    - a__x
  tool_groups:
    read:
      - a__list
  allow_on_eval_failure: true
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "test-pol", cfg.Policy.Version)
	require.Equal(t, []string{"a__x"}, cfg.Policy.ElevatedTools)
	require.Equal(t, []string{"a__list"}, cfg.Policy.ToolGroups["read"])
	require.True(t, cfg.Policy.AllowOnRARParseFailure)
	require.True(t, cfg.PolicyHardenSchemas())
	require.Equal(t, 4096, cfg.Policy.MaxArgumentBytes)
	require.Equal(t, 10, cfg.Policy.MaxArgumentDepth)
	require.Equal(t, 99, cfg.Policy.MaxArgumentKeys)
}

func TestQdrantCollectionDefault(t *testing.T) {
	var c GatewayConfig
	require.Equal(t, defaults.DefaultQdrantCollectionName, c.QdrantCollection())
	c.Qdrant.Collection = "custom"
	require.Equal(t, "custom", c.QdrantCollection())
}

func TestAggregationTimeoutsFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
aggregation:
  init_timeout: 3s
  list_timeout: 7s
  call_timeout: 90s
  max_in_flight: 5
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 3*time.Second, cfg.AggregationInitTimeout())
	require.Equal(t, 7*time.Second, cfg.AggregationListTimeout())
	require.Equal(t, 90*time.Second, cfg.AggregationCallTimeout())
	require.Equal(t, 5, cfg.AggregationMaxInFlight())
}

func TestAggregationTimeoutsDefault(t *testing.T) {
	var c GatewayConfig
	require.Equal(t, defaults.MultiplexInitTimeout, c.AggregationInitTimeout())
	require.Equal(t, defaults.MultiplexListTimeout, c.AggregationListTimeout())
	require.Equal(t, defaults.MultiplexCallTimeout, c.AggregationCallTimeout())
}

func TestAggregationTimeoutsIgnoreInvalid(t *testing.T) {
	c := GatewayConfig{Aggregation: AggregationSettings{
		InitTimeout: "not-a-duration",
		ListTimeout: "0s",
		CallTimeout: "",
	}}
	require.Equal(t, defaults.MultiplexInitTimeout, c.AggregationInitTimeout())
	require.Equal(t, defaults.MultiplexListTimeout, c.AggregationListTimeout())
	require.Equal(t, defaults.MultiplexCallTimeout, c.AggregationCallTimeout())
}

func TestAggregationMaxInFlightDefaultAndInvalid(t *testing.T) {
	var c GatewayConfig
	require.Equal(t, 0, c.AggregationMaxInFlight())

	c.Aggregation.MaxInFlight = -7
	require.Equal(t, 0, c.AggregationMaxInFlight())
}

func TestApplyEnvOverridesPolicyAllowOnEvalFailureTrue(t *testing.T) {
	cfg := GatewayConfig{}
	t.Setenv("POLICY_ALLOW_ON_EVAL_FAILURE", "true")

	cfg.ApplyEnvOverrides()
	require.True(t, cfg.Policy.AllowOnRARParseFailure)
}

func TestApplyEnvOverridesPolicyAllowOnEvalFailureFalse(t *testing.T) {
	cfg := GatewayConfig{
		Policy: PolicySettings{
			AllowOnRARParseFailure: true,
		},
	}
	t.Setenv("POLICY_ALLOW_ON_EVAL_FAILURE", "false")

	cfg.ApplyEnvOverrides()
	require.False(t, cfg.Policy.AllowOnRARParseFailure)
}

func TestApplyEnvOverridesAggregationMaxInFlight(t *testing.T) {
	cfg := GatewayConfig{
		Aggregation: AggregationSettings{
			MaxInFlight: 2,
		},
	}
	t.Setenv("AGGREGATION_MAX_IN_FLIGHT", "9")

	cfg.ApplyEnvOverrides()
	require.Equal(t, 9, cfg.AggregationMaxInFlight())
}

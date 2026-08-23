package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestRateLimitDefaultsWhenUnsetOrZero(t *testing.T) {
	var cfg config.GatewayConfig
	rl := rateLimitConfig(cfg)
	require.False(t, rl.Enabled)
	require.Equal(t, float64(defaults.DefaultRateLimitRPS), rl.RPS)
	require.Equal(t, defaults.DefaultRateLimitBurst, rl.Burst)

	cfg = config.GatewayConfig{
		RateLimitCfg: config.RateLimitSettings{
			Enabled: true,
			RPS:     0,
			Burst:   0,
		},
	}
	rl = rateLimitConfig(cfg)
	require.True(t, rl.Enabled)
	require.Equal(t, float64(defaults.DefaultRateLimitRPS), rl.RPS)
	require.Equal(t, defaults.DefaultRateLimitBurst, rl.Burst)
}

func TestRateLimitFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://127.0.0.1:1
rate_limit:
  enabled: true
  rps: 42.5
  burst: 64
`), 0o644)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("RATE_LIMIT_ENABLED", "")
	t.Setenv("RATE_LIMIT_RPS", "")
	t.Setenv("RATE_LIMIT_BURST", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	rl := rateLimitConfig(cfg)
	require.True(t, rl.Enabled)
	require.Equal(t, 42.5, rl.RPS)
	require.Equal(t, 64, rl.Burst)
}

func TestRateLimitEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://127.0.0.1:1
rate_limit:
  enabled: true
  rps: 11
  burst: 22
`), 0o644)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("RATE_LIMIT_RPS", "120.5")
	t.Setenv("RATE_LIMIT_BURST", "333")

	cfg, err := config.Load()
	require.NoError(t, err)
	rl := rateLimitConfig(cfg)
	require.False(t, rl.Enabled)
	require.Equal(t, 120.5, rl.RPS)
	require.Equal(t, 333, rl.Burst)
}

func TestRateLimitInvalidYAMLValuesFallbackToDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://127.0.0.1:1
rate_limit:
  enabled: true
  rps: -5
  burst: -10
`), 0o644)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("RATE_LIMIT_ENABLED", "")
	t.Setenv("RATE_LIMIT_RPS", "")
	t.Setenv("RATE_LIMIT_BURST", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	rl := rateLimitConfig(cfg)
	require.True(t, rl.Enabled)
	require.Equal(t, float64(defaults.DefaultRateLimitRPS), rl.RPS)
	require.Equal(t, defaults.DefaultRateLimitBurst, rl.Burst)
}

func TestPolicyArgumentLimitsDefaults(t *testing.T) {
	var cfg config.GatewayConfig
	lim := argumentLimits(cfg)
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

	cfg, err := config.Load()
	require.NoError(t, err)
	lim := argumentLimits(cfg)
	require.Equal(t, 1234, lim.MaxBytes)
	require.Equal(t, 12, lim.MaxDepth)
	require.Equal(t, 77, lim.MaxKeys)
}

func TestPolicyArgumentLimitsZeroFallsBackToDefaults(t *testing.T) {
	cfg := config.GatewayConfig{
		Policy: config.PolicySettings{
			MaxArgumentBytes: 0,
			MaxArgumentDepth: -1,
			MaxArgumentKeys:  0,
		},
	}
	lim := argumentLimits(cfg)
	require.Equal(t, defaults.MaxToolArgumentsJSONBytes, lim.MaxBytes)
	require.Equal(t, defaults.MaxToolArgumentsJSONDepth, lim.MaxDepth)
	require.Equal(t, defaults.MaxToolArgumentsJSONKeys, lim.MaxKeys)
}

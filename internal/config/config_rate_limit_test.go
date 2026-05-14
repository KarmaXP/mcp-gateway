package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestRateLimitDefaultsWhenUnsetOrZero(t *testing.T) {
	var cfg GatewayConfig
	rl := cfg.RateLimit()
	require.False(t, rl.Enabled)
	require.Equal(t, float64(defaults.DefaultRateLimitRPS), rl.RPS)
	require.Equal(t, defaults.DefaultRateLimitBurst, rl.Burst)

	cfg = GatewayConfig{
		RateLimitCfg: RateLimitSettings{
			Enabled: true,
			RPS:     0,
			Burst:   0,
		},
	}
	rl = cfg.RateLimit()
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

	cfg, err := Load()
	require.NoError(t, err)
	rl := cfg.RateLimit()
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

	cfg, err := Load()
	require.NoError(t, err)
	rl := cfg.RateLimit()
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

	cfg, err := Load()
	require.NoError(t, err)
	rl := cfg.RateLimit()
	require.True(t, rl.Enabled)
	require.Equal(t, float64(defaults.DefaultRateLimitRPS), rl.RPS)
	require.Equal(t, defaults.DefaultRateLimitBurst, rl.Burst)
}

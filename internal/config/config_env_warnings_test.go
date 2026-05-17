package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyEnvOverridesIgnoresInvalidRouterMode(t *testing.T) {
	var cfg GatewayConfig
	cfg.SemanticRouter.Mode = "off"
	t.Setenv("ROUTER_MODE", "bogus")
	cfg.ApplyEnvOverrides()
	require.Equal(t, "off", cfg.SemanticRouter.Mode)
}

func TestApplyEnvOverridesIgnoresInvalidRouterTopK(t *testing.T) {
	var cfg GatewayConfig
	cfg.SemanticRouter.TopK = 7
	t.Setenv("ROUTER_TOP_K", "nope")
	cfg.ApplyEnvOverrides()
	require.Equal(t, 7, cfg.SemanticRouter.TopK)
}

func TestApplyEnvOverridesIgnoresInvalidRateLimitRPS(t *testing.T) {
	var cfg GatewayConfig
	cfg.RateLimitCfg.RPS = 12.5
	t.Setenv("RATE_LIMIT_RPS", "0")
	cfg.ApplyEnvOverrides()
	require.Equal(t, 12.5, cfg.RateLimitCfg.RPS)
}

func TestApplyEnvOverridesAcceptsValidRouterMode(t *testing.T) {
	var cfg GatewayConfig
	t.Setenv("ROUTER_MODE", "filter_list")
	cfg.ApplyEnvOverrides()
	require.Equal(t, "filter_list", cfg.SemanticRouter.Mode)
}

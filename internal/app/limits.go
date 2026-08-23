package app

import (
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/validate"
)

func rateLimitConfig(c config.GatewayConfig) ratelimit.Config {
	rps := c.RateLimitCfg.RPS
	if rps <= 0 {
		rps = float64(defaults.DefaultRateLimitRPS)
	}
	burst := c.RateLimitCfg.Burst
	if burst <= 0 {
		burst = defaults.DefaultRateLimitBurst
	}
	return ratelimit.Config{
		Enabled: c.RateLimitCfg.Enabled,
		RPS:     rps,
		Burst:   burst,
	}
}

func argumentLimits(c config.GatewayConfig) validate.Limits {
	dl := validate.DefaultLimits()
	out := validate.Limits{
		MaxBytes: c.Policy.MaxArgumentBytes,
		MaxDepth: c.Policy.MaxArgumentDepth,
		MaxKeys:  c.Policy.MaxArgumentKeys,
	}
	if out.MaxBytes <= 0 {
		out.MaxBytes = dl.MaxBytes
	}
	if out.MaxDepth <= 0 {
		out.MaxDepth = dl.MaxDepth
	}
	if out.MaxKeys <= 0 {
		out.MaxKeys = dl.MaxKeys
	}
	return out
}

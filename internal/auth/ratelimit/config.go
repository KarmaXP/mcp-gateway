package ratelimit

import (
	"os"
	"strconv"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

type Config struct {
	Enabled bool
	RPS     float64
	Burst   int
}

func FromEnvironment() Config {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("RATE_LIMIT_ENABLED")))
	enabled := v == "1" || v == "true" || v == "yes"
	rps := float64(defaults.DefaultRateLimitRPS)
	if s := strings.TrimSpace(os.Getenv("RATE_LIMIT_RPS")); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
			rps = f
		}
	}
	burst := defaults.DefaultRateLimitBurst
	if s := strings.TrimSpace(os.Getenv("RATE_LIMIT_BURST")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			burst = n
		}
	}
	return Config{Enabled: enabled, RPS: rps, Burst: burst}
}

package ratelimit

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

const defaultMaxBuckets = 10_000

type Config struct {
	Enabled       bool
	RPS           float64
	Burst         int
	BucketIdleTTL time.Duration // zero = defaults.RateLimitBucketIdleTTL
	MaxBuckets    int           // zero = defaultMaxBuckets
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
	maxBuckets := defaultMaxBuckets
	if s := strings.TrimSpace(os.Getenv("RATE_LIMIT_MAX_BUCKETS")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxBuckets = n
		}
	}
	return Config{Enabled: enabled, RPS: rps, Burst: burst, MaxBuckets: maxBuckets}
}

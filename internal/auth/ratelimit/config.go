package ratelimit

import "time"

const defaultMaxBuckets = 10_000

type Config struct {
	Enabled       bool
	RPS           float64
	Burst         int
	BucketIdleTTL time.Duration // zero = defaults.RateLimitBucketIdleTTL
	MaxBuckets    int           // zero = defaultMaxBuckets
}

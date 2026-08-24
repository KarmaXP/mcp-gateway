package ratelimit

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/codes"
	"golang.org/x/time/rate"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

const (
	perRequestKeyPrefix = "req:"
	authFailureKeyPrefix = "authfail:"
)

// Limiter owns the buckets and their eviction, shared by the per-request limit and the
// authentication failure budget.
type Limiter struct {
	enabled      bool
	buckets      *bucketMap
	rps          rate.Limit
	burst        int
	failureRPS   rate.Limit
	failureBurst int
}

// New starts the bucket eviction loop, which stops when ctx is done.
func New(ctx context.Context, cfg Config) *Limiter {
	rps := rate.Limit(cfg.RPS)
	if rps <= 0 {
		rps = rate.Limit(defaults.DefaultRateLimitRPS)
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = defaults.DefaultRateLimitBurst
	}
	idleTTL := cfg.BucketIdleTTL
	if idleTTL <= 0 {
		idleTTL = defaults.RateLimitBucketIdleTTL
	}
	l := &Limiter{
		enabled:      cfg.Enabled,
		buckets:      newBucketMap(cfg.MaxBuckets, idleTTL),
		rps:          rps,
		burst:        burst,
		failureRPS:   rate.Limit(defaults.AuthFailureBudgetRPS),
		failureBurst: defaults.AuthFailureBudgetBurst,
	}
	go l.evictIdleBuckets(ctx, evictionInterval(idleTTL))
	return l
}

// AllowAuthAttempt is checked before any signature verification, and spends nothing:
// only a failure costs the client a token.
func (l *Limiter) AllowAuthAttempt(r *http.Request) bool {
	if l == nil {
		return true
	}
	return l.buckets.hasTokens(authFailureKeyPrefix+clientIP(r), time.Now())
}

// RecordAuthFailure spends one token of the client's failure budget.
func (l *Limiter) RecordAuthFailure(r *http.Request) {
	if l == nil {
		return
	}
	l.buckets.allow(authFailureKeyPrefix+clientIP(r), l.failureRPS, l.failureBurst, time.Now())
}

// Middleware is the per-request limit, keyed by subject and disabled unless cfg.Enabled.
func (l *Limiter) Middleware() func(http.Handler) http.Handler {
	if l == nil || !l.enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if !l.buckets.allow(perRequestKeyPrefix+limiterKey(r), l.rps, l.burst, time.Now()) {
				telemetry.RecordRateLimit(r.Context(), false)
				telemetry.EndHostRPCSpanIfOpen(r.Context(), codes.Error, "rate limited")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			telemetry.RecordRateLimit(r.Context(), true)
			next.ServeHTTP(w, r)
		})
	}
}

func (l *Limiter) evictIdleBuckets(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			l.buckets.sweepStale(now)
		}
	}
}

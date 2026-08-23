package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"golang.org/x/time/rate"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

type bucketEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// Per-subject (or RemoteAddr) token bucket when cfg.Enabled.
func HTTPMiddleware(cfg Config) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	lim := rate.Limit(cfg.RPS)
	if lim <= 0 {
		lim = rate.Limit(defaults.DefaultRateLimitRPS)
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = defaults.DefaultRateLimitBurst
	}
	idleTTL := cfg.BucketIdleTTL
	if idleTTL <= 0 {
		idleTTL = defaults.RateLimitBucketIdleTTL
	}
	maxBuckets := cfg.MaxBuckets
	if maxBuckets <= 0 {
		maxBuckets = defaultMaxBuckets
	}
	store := newBucketMap(maxBuckets, idleTTL)
	go func() {
		ticker := time.NewTicker(evictionInterval(idleTTL))
		defer ticker.Stop()
		for now := range ticker.C {
			store.sweepStale(now)
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			key := limiterKey(r)
			if !store.allow(key, lim, burst, time.Now()) {
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

func shouldSkipRateLimit(path string) bool {
	if path == mcpwire.PathMCPSSE || path == mcpwire.PathHealthz || path == mcpwire.PathReadyz {
		return true
	}
	return strings.HasPrefix(path, mcpwire.PathHealthz+"/") || strings.HasPrefix(path, mcpwire.PathReadyz+"/")
}

func limiterKey(r *http.Request) string {
	if sub := hostctx.SubjectIDFromContext(r.Context()); sub != "" {
		return "sub:" + sub
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return "anon"
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		return "ip:" + host
	}
	return "ip:" + addr
}

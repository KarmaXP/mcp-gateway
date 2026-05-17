package ratelimit

import (
	"net/http"
	"strings"
	"sync"
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
	var mu sync.Mutex
	buckets := make(map[string]*bucketEntry)

	evictStale := func(now time.Time) {
		for key, e := range buckets {
			if now.Sub(e.lastSeen) > idleTTL {
				delete(buckets, key)
			}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			key := limiterKey(r)
			now := time.Now()
			mu.Lock()
			evictStale(now)
			e, ok := buckets[key]
			if !ok {
				e = &bucketEntry{lim: rate.NewLimiter(lim, burst), lastSeen: now}
				buckets[key] = e
			}
			e.lastSeen = now
			allow := e.lim.Allow()
			mu.Unlock()
			if !allow {
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
	if path == mcpwire.PathHealthz || path == mcpwire.PathReadyz {
		return true
	}
	return strings.HasPrefix(path, mcpwire.PathHealthz+"/") || strings.HasPrefix(path, mcpwire.PathReadyz+"/")
}

func limiterKey(r *http.Request) string {
	if sub := hostctx.SubjectIDFromContext(r.Context()); sub != "" {
		return "sub:" + sub
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr != "" {
		return "ip:" + addr
	}
	return "anon"
}

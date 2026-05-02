package ratelimit

import (
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

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
	var mu sync.Mutex
	buckets := make(map[string]*rate.Limiter)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			key := limiterKey(r)
			mu.Lock()
			b, ok := buckets[key]
			if !ok {
				b = rate.NewLimiter(lim, burst)
				buckets[key] = b
			}
			mu.Unlock()
			if !b.Allow() {
				telemetry.RecordRateLimit(r.Context(), false)
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

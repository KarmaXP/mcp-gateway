package ratelimit

import (
	"net"
	"net/http"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
)

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
	return "ip:" + clientIP(r)
}

// X-Forwarded-For is deliberately ignored: trusting it would make the per-IP key spoofable.
func clientIP(r *http.Request) string {
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return "anon"
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		return host
	}
	return addr
}

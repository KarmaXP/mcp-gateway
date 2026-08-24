package orchestrator

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func HTTPServerOptions(serviceName string, authCfg auth.JWTAuthConfig, v *auth.Validator, pol *policy.Holder, limiter *ratelimit.Limiter) []httpserver.Option {
	if serviceName == "" {
		serviceName = "mcp-gateway"
	}
	// Outermost: HTTP server span; then JWT (may start mcp.host.request for POST /mcp/rpc); then rate limit; then mux.
	// The failure budget lives inside the JWT middleware, since only a failed verification spends it.
	return []httpserver.Option{
		httpserver.WithHTTPMiddleware(func(h http.Handler) http.Handler {
			return otelhttp.NewHandler(h, serviceName,
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return r.Method + " " + r.URL.Path
				}),
			)
		}),
		httpserver.WithHTTPMiddleware(auth.HTTPMiddleware(authCfg, v, pol, limiter)),
		httpserver.WithHTTPMiddleware(limiter.Middleware()),
	}
}

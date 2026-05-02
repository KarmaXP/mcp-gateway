package orchestrator

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func HTTPServerOptions(serviceName string, authCfg auth.JWTAuthConfig, v *auth.Validator, pol *policy.Holder, rl ratelimit.Config) []httpserver.Option {
	if serviceName == "" {
		serviceName = "mcp-gateway"
	}
	return []httpserver.Option{
		httpserver.WithHTTPMiddleware(auth.HTTPMiddleware(authCfg, v, pol)),
		httpserver.WithHTTPMiddleware(ratelimit.HTTPMiddleware(rl)),
		httpserver.WithHTTPMiddleware(func(h http.Handler) http.Handler {
			return otelhttp.NewHandler(h, serviceName,
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return r.Method + " " + r.URL.Path
				}),
			)
		}),
	}
}

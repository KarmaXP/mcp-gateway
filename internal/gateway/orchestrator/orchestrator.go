// Package orchestrator wires the production HTTP pipeline: Auth → OpenTelemetry → gateway mux.
package orchestrator

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
)

// HTTPMiddlewareOptions returns middleware in outer→inner order: auth, then OTel HTTP tracing, then the gateway mux.
func HTTPMiddlewareOptions(serviceName string, authCfg auth.Config, v *auth.Validator) []httpserver.Option {
	if serviceName == "" {
		serviceName = "mcp-gateway"
	}
	return []httpserver.Option{
		httpserver.WithHandlerMiddleware(auth.HTTPMiddleware(authCfg, v)),
		httpserver.WithHandlerMiddleware(func(h http.Handler) http.Handler {
			return otelhttp.NewHandler(h, serviceName,
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return r.Method + " " + r.URL.Path
				}),
			)
		}),
	}
}

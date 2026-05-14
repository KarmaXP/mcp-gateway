package httpserver

import (
	"net/http"
	"strings"
)

// WithOriginAllowList validates Origin on MCP ingress when an allow-list is configured.
func WithOriginAllowList(origins []string) Option {
	allow := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		allow[trimmed] = struct{}{}
	}
	if len(allow) == 0 {
		return func(*Server) {}
	}

	return WithHTTPMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isOriginCheckedRoute(r) {
				next.ServeHTTP(w, r)
				return
			}

			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := allow[origin]; ok {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "origin not allowed", http.StatusForbidden)
		})
	})
}

func isOriginCheckedRoute(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == PathMCPSSE) ||
		(r.Method == http.MethodPost && r.URL.Path == PathMCPRPC)
}

package auth

import (
	"net/http"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
)

// Prefix length for "bearer " (Authorization header scheme, case-insensitive check on a lowercased copy).
const bearerAuthSchemeLowerLen = 7

func HTTPMiddleware(cfg JWTAuthConfig, v *Validator) func(http.Handler) http.Handler {
	if cfg.Mode == "" || cfg.Mode == "none" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range cfg.SkipPathPrefixes {
				if p != "" && strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			raw := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			tok := strings.TrimSpace(raw[bearerAuthSchemeLowerLen:])
			if tok == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if v == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			tools, err := v.ValidateWithAllowedTools(r.Context(), tok)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := hostctx.WithAllowedToolNames(r.Context(), tools)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

package auth

import (
	"net/http"
	"strings"
)

// HTTPMiddleware enforces bearer JWT on MCP routes (health paths skipped).
func HTTPMiddleware(cfg Config, v *Validator) func(http.Handler) http.Handler {
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
			tok := strings.TrimSpace(raw[7:])
			if tok == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if v == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err := v.Validate(r.Context(), tok); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

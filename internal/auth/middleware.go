package auth

import (
	"net/http"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

const bearerAuthSchemeLowerLen = 7 // len("bearer ") after strings.ToLower on Authorization

func HTTPMiddleware(cfg JWTAuthConfig, v *Validator, pol *policy.Holder) func(http.Handler) http.Handler {
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
			claims, err := v.ParseTokenClaims(r.Context(), tok)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var eng *policy.Engine
			if pol != nil {
				eng = pol.Load()
			}
			tools, err := effectiveAllowList(eng, claims)
			if err != nil {
				telemetry.RecordPolicyDecision(r.Context(), defaults.MetricPolicyOutcomeDeny, defaults.MetricPolicyReasonPolicyEvalFailed)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := hostctx.WithAllowedToolNames(r.Context(), tools)
			if sub := claims.Subject(); sub != "" {
				ctx = hostctx.WithSubjectID(ctx, sub)
			}
			if eng != nil {
				ctx = hostctx.WithPolicyVersion(ctx, eng.Version())
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func effectiveAllowList(pol *policy.Engine, claims *TokenClaims) ([]string, error) {
	if pol == nil {
		return claims.NormalizedMcpTools(), nil
	}
	return pol.EffectiveAllowList(claims)
}

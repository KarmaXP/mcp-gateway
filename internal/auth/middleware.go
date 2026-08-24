package auth

import (
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
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

			secStart := time.Now()
			ctx := r.Context()
			var hSpan trace.Span
			if r.Method == http.MethodPost && r.URL.Path == mcpwire.PathMCPRPC {
				ctx, hSpan = telemetry.StartSpan(ctx, telemetry.SpanMCPHostRequest)
				ctx = telemetry.CtxWithHostRPCStarted(ctx)
			}

			endHostErr := func(msg string) {
				if hSpan != nil {
					hSpan.SetStatus(codes.Error, msg)
					hSpan.End()
				}
			}

			raw := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
				telemetry.RecordInternalPhase(ctx, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(secStart))
				endHostErr("missing bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			tok := strings.TrimSpace(raw[bearerAuthSchemeLowerLen:])
			if tok == "" || v == nil {
				telemetry.RecordInternalPhase(ctx, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(secStart))
				endHostErr("unauthorized")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			actx, authnSpan := telemetry.StartSpan(ctx, telemetry.SpanSecurityAuthn)
			claims, err := v.ParseTokenClaims(actx, tok)
			if err != nil {
				authnSpan.RecordError(err)
				authnSpan.SetStatus(codes.Error, "invalid token")
				authnSpan.End()
				telemetry.RecordInternalPhase(actx, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(secStart))
				endHostErr("unauthorized")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			authnSpan.SetStatus(codes.Ok, "")
			authnSpan.End()

			var eng *policy.Engine
			if pol != nil {
				eng = pol.Load()
			}
			tools, err := eng.EffectiveAllowList(claims)
			if err != nil {
				telemetry.RecordPolicyDecision(ctx, defaults.MetricPolicyOutcomeDeny, defaults.MetricPolicyReasonPolicyEvalFailed)
				telemetry.RecordInternalPhase(ctx, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(secStart))
				endHostErr("policy")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx2 := hostctx.WithAllowedToolNames(ctx, tools)
			if sub := claims.Subject(); sub != "" {
				ctx2 = hostctx.WithSubjectID(ctx2, sub)
			}
			if eng != nil {
				ctx2 = hostctx.WithPolicyVersion(ctx2, eng.Version())
			}
			telemetry.RecordInternalPhase(ctx2, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(secStart))
			next.ServeHTTP(w, r.WithContext(ctx2))
		})
	}
}

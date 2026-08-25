package auth

import (
	"context"
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

// AuthAttemptLimiter bounds signature verification per client: only a failure spends budget.
type AuthAttemptLimiter interface {
	AllowAuthAttempt(r *http.Request) bool
	RecordAuthFailure(r *http.Request)
}

func HTTPMiddleware(cfg JWTAuthConfig, v *Validator, pol *policy.Holder, limiter AuthAttemptLimiter) func(http.Handler) http.Handler {
	if cfg.Mode == "" || cfg.Mode == "none" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return &authMiddleware{cfg: cfg, validator: v, policy: pol, limiter: limiter, next: next}
	}
}

type authMiddleware struct {
	cfg       JWTAuthConfig
	validator *Validator
	policy    *policy.Holder
	limiter   AuthAttemptLimiter
	next      http.Handler
}

type authAttempt struct {
	ctx      context.Context
	started  time.Time
	hostSpan trace.Span
}

func (m *authMiddleware) skips(path string) bool {
	for _, p := range m.cfg.SkipPathPrefixes {
		if p != "" && strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func (m *authMiddleware) begin(r *http.Request) *authAttempt {
	att := &authAttempt{ctx: r.Context(), started: time.Now()}
	if r.Method == http.MethodPost && r.URL.Path == mcpwire.PathMCPRPC {
		att.ctx, att.hostSpan = telemetry.StartSpan(att.ctx, telemetry.SpanMCPHostRequest)
		att.ctx = telemetry.CtxWithHostRPCStarted(att.ctx)
	}
	return att
}

func (a *authAttempt) reject(w http.ResponseWriter, status int, message, spanReason string) {
	telemetry.RecordInternalPhase(a.ctx, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(a.started))
	if a.hostSpan != nil {
		a.hostSpan.SetStatus(codes.Error, spanReason)
		a.hostSpan.End()
	}
	http.Error(w, message, status)
}

func (m *authMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.skips(r.URL.Path) {
		m.next.ServeHTTP(w, r)
		return
	}
	att := m.begin(r)

	raw := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		att.reject(w, http.StatusUnauthorized, "unauthorized", "missing bearer")
		return
	}
	token := strings.TrimSpace(raw[bearerAuthSchemeLowerLen:])
	if token == "" || m.validator == nil {
		att.reject(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	if m.limiter != nil && !m.limiter.AllowAuthAttempt(r) {
		att.reject(w, http.StatusTooManyRequests, "too many failed authentications", "too many failed authentications")
		return
	}

	claims, err := m.authenticate(att.ctx, r, token)
	if err != nil {
		att.reject(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	eng := m.engine()
	tools, err := eng.EffectiveAllowList(claims)
	if err != nil {
		telemetry.RecordPolicyDecision(att.ctx, defaults.MetricPolicyOutcomeDeny, defaults.MetricPolicyReasonPolicyEvalFailed)
		att.reject(w, http.StatusUnauthorized, "unauthorized", "policy")
		return
	}

	ctx := hostctx.WithAllowedToolNames(att.ctx, tools)
	if sub := claims.Subject(); sub != "" {
		ctx = hostctx.WithSubjectID(ctx, sub)
	}
	if eng != nil {
		ctx = hostctx.WithPolicyVersion(ctx, eng.Version())
	}
	telemetry.RecordInternalPhase(ctx, defaults.MetricInternalMethodUnknown, defaults.MetricInternalPhaseSecurity, time.Since(att.started))
	m.next.ServeHTTP(w, r.WithContext(ctx))
}

func (m *authMiddleware) authenticate(ctx context.Context, r *http.Request, token string) (*TokenClaims, error) {
	actx, span := telemetry.StartSpan(ctx, telemetry.SpanSecurityAuthn)
	defer span.End()
	claims, err := m.validator.ParseTokenClaims(actx, token)
	if err != nil {
		if m.limiter != nil {
			m.limiter.RecordAuthFailure(r)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid token")
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return claims, nil
}

func (m *authMiddleware) engine() *policy.Engine {
	if m.policy == nil {
		return nil
	}
	return m.policy.Load()
}

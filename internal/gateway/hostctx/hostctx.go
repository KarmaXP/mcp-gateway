package hostctx

import (
	"context"
	"strings"
)

// HeaderMCPIntent is the HTTP header carrying optional natural-language intent for semantic routing.
const HeaderMCPIntent = "X-MCP-Intent"

type clientIntentKey struct{}

// WithClientIntent attaches trimmed client-supplied intent text to the request context.
func WithClientIntent(ctx context.Context, intent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, clientIntentKey{}, strings.TrimSpace(intent))
}

// ClientIntentFromContext returns intent text set with WithClientIntent, or "".
func ClientIntentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(clientIntentKey{}).(string)
	return s
}

type allowedToolNamesKey struct{}

// WithAllowedToolNames attaches a copy of namespaced tool names the caller may invoke (JWT / policy).
func WithAllowedToolNames(parent context.Context, toolNames []string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	cp := normalizeAllowedToolNames(toolNames)
	if len(cp) == 0 {
		return parent
	}
	return context.WithValue(parent, allowedToolNamesKey{}, cp)
}

// AllowedToolNamesFromContext returns names from WithAllowedToolNames, or nil if unset.
func AllowedToolNamesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	tools, _ := ctx.Value(allowedToolNamesKey{}).([]string)
	if len(tools) == 0 {
		return nil
	}
	out := append([]string(nil), tools...)
	return out
}

type subjectIDKey struct{}

// WithSubjectID attaches a JWT subject (sub) for audit correlation; do not log raw values (hash in audit layer).
func WithSubjectID(parent context.Context, subject string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return parent
	}
	return context.WithValue(parent, subjectIDKey{}, subject)
}

// SubjectIDFromContext returns the subject set with WithSubjectID, or "".
func SubjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(subjectIDKey{}).(string)
	return s
}

type policyVersionKey struct{}

// WithPolicyVersion attaches the active policy configuration version for audit and traces.
func WithPolicyVersion(parent context.Context, version string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return parent
	}
	return context.WithValue(parent, policyVersionKey{}, version)
}

// PolicyVersionFromContext returns the policy version from WithPolicyVersion, or "".
func PolicyVersionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(policyVersionKey{}).(string)
	return s
}

func normalizeAllowedToolNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

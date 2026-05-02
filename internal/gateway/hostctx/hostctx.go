package hostctx

import (
	"context"
	"strings"
)

// Optional natural-language hint for semantic routing (HTTP header name).
const HeaderMCPIntent = "X-MCP-Intent"

type clientIntentKey struct{}

func WithClientIntent(ctx context.Context, intent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, clientIntentKey{}, strings.TrimSpace(intent))
}

func ClientIntentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(clientIntentKey{}).(string)
	return s
}

type allowedToolNamesKey struct{}

// Namespaced tool ids the principal may call (from JWT and/or policy merge).
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

// JWT sub for audit paths — hash before logging (SEC5).
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

func SubjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(subjectIDKey{}).(string)
	return s
}

type policyVersionKey struct{}

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

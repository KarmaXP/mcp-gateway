// Package ingress carries request-scoped metadata from the HTTP transport into MCP dispatch (e.g. semantic routing intent).
package ingress

import (
	"context"
	"strings"
)

// HeaderMCPIntent is the optional HTTP header whose value is forwarded as router.RoutingSignal.IntentText on tools/call.
const HeaderMCPIntent = "X-MCP-Intent"

type mcpIntentKey struct{}

// WithMCPIntent returns a child context carrying trimmed intent text for Aggregator.ToolsCall / the semantic router.
func WithMCPIntent(ctx context.Context, intent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, mcpIntentKey{}, strings.TrimSpace(intent))
}

// MCPIntentFromContext returns text set via WithMCPIntent, or "".
func MCPIntentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(mcpIntentKey{}).(string)
	return s
}

type allowedToolsKey struct{}

// WithAllowedTools attaches a JWT-derived allow-list of namespaced tool ids for semantic routing (§3.B).
// Nil or empty slice means no restriction.
func WithAllowedTools(parent context.Context, tools []string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	cp := normalizeAllowedTools(tools)
	if len(cp) == 0 {
		return parent
	}
	return context.WithValue(parent, allowedToolsKey{}, cp)
}

// AllowedToolsFromContext returns a copy of tools set via WithAllowedTools, or nil if unset / empty.
func AllowedToolsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	tools, _ := ctx.Value(allowedToolsKey{}).([]string)
	if len(tools) == 0 {
		return nil
	}
	out := append([]string(nil), tools...)
	return out
}

func normalizeAllowedTools(in []string) []string {
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

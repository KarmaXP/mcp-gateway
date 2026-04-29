package ingress

import (
	"context"
	"strings"
)

const HeaderMCPIntent = "X-MCP-Intent"

type mcpIntentKey struct{}

func WithMCPIntent(ctx context.Context, intent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, mcpIntentKey{}, strings.TrimSpace(intent))
}

func MCPIntentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(mcpIntentKey{}).(string)
	return s
}

type allowedToolsKey struct{}

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

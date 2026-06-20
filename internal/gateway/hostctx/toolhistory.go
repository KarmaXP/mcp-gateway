package hostctx

import (
	"context"
	"strings"
)

type mcpSessionIDKey struct{}

func WithMCPSessionID(parent context.Context, id string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return parent
	}
	return context.WithValue(parent, mcpSessionIDKey{}, id)
}

func MCPSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(mcpSessionIDKey{}).(string)
	return s
}

type SuccessfulToolCallRecorder interface {
	RecordSuccessfulToolCall(namespaced string)
}

type toolCallRecorderKey struct{}

func WithToolCallRecorder(parent context.Context, r SuccessfulToolCallRecorder) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if r == nil {
		return parent
	}
	return context.WithValue(parent, toolCallRecorderKey{}, r)
}

func RecordSuccessfulToolCall(ctx context.Context, namespaced string) {
	if ctx == nil || namespaced == "" {
		return
	}
	r, _ := ctx.Value(toolCallRecorderKey{}).(SuccessfulToolCallRecorder)
	if r != nil {
		r.RecordSuccessfulToolCall(namespaced)
	}
}

type recentToolNamesKey struct{}

func WithRecentToolNames(parent context.Context, names []string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if len(names) == 0 {
		return parent
	}
	cp := append([]string(nil), names...)
	return context.WithValue(parent, recentToolNamesKey{}, cp)
}

func RecentToolNamesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(recentToolNamesKey{}).([]string)
	if len(s) == 0 {
		return nil
	}
	out := append([]string(nil), s...)
	return out
}

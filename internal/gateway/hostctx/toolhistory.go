package hostctx

import (
	"context"
	"strings"
)

type mcpSessionIDKey struct{}

// WithMCPSessionID attaches the host SSE session id for routing telemetry.
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

// MCPSessionIDFromContext returns the session id if present.
func MCPSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(mcpSessionIDKey{}).(string)
	return s
}

// SuccessfulToolCallRecorder is invoked by the multiplexer after a successful tools/call upstream result.
type SuccessfulToolCallRecorder interface {
	RecordSuccessfulToolCall(namespaced string)
}

type toolCallRecorderKey struct{}

// WithToolCallRecorder attaches a per-session recorder (typically *session.Session).
func WithToolCallRecorder(parent context.Context, r SuccessfulToolCallRecorder) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if r == nil {
		return parent
	}
	return context.WithValue(parent, toolCallRecorderKey{}, r)
}

// RecordSuccessfulToolCall notifies the session recorder, if any.
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

// WithRecentToolNames passes the last N successful tools/call names for semantic routing (newest last).
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

// RecentToolNamesFromContext returns names attached for routing (may be nil).
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

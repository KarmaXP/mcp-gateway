package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// EndHostRPCSpanIfOpen ends mcp.host.request when JWT middleware opened it for POST /mcp/rpc (e.g. rate-limit reject before handler runs).
func EndHostRPCSpanIfOpen(ctx context.Context, c codes.Code, msg string) {
	if !HostRPCStartedFromContext(ctx) {
		return
	}
	s := trace.SpanFromContext(ctx)
	if s == nil || !s.IsRecording() {
		return
	}
	if c != codes.Unset && c != codes.Ok {
		s.SetStatus(c, msg)
	}
	s.End()
}

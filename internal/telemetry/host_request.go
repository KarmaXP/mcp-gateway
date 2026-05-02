package telemetry

import "context"

// Marks that mcp.host.request was started in auth middleware for this RPC (so httpserver does not start a second root).
type hostRPCStartedKey struct{}

func CtxWithHostRPCStarted(ctx context.Context) context.Context {
	return context.WithValue(ctx, hostRPCStartedKey{}, true)
}

func HostRPCStartedFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(hostRPCStartedKey{}).(bool)
	return ok && v
}

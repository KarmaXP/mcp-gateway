package upstream

import (
	"context"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

// One MCP server the gateway multiplexes to (stdio or HTTP+SSE).
type Client interface {
	ID() string
	Prefix() string
	Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error)
}

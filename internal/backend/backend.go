package backend

import (
	"context"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

// Upstream is one configured MCP server the gateway multiplexes to (stdio or HTTP+SSE transport).
type Upstream interface {
	ID() string
	Prefix() string
	Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error)
}

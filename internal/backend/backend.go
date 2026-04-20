// Package backend defines the upstream MCP adapter surface (one logical backend per configured prefix).
package backend

import (
	"context"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

// Caller invokes JSON-RPC on a connected backend session (stdio, HTTP+SSE, etc.).
type Caller interface {
	// Call performs one JSON-RPC round-trip. The implementation must honor ctx deadline/cancel.
	Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error)
}

// Backend is a configured upstream with stable id and namespace prefix (R1).
type Backend interface {
	ID() string
	Prefix() string
	Caller
}

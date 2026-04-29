package backend

import (
	"context"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type Caller interface {
	Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error)
}

type Backend interface {
	ID() string
	Prefix() string
	Caller
}

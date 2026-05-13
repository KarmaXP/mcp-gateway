package multiplex

import (
	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func (a *Multiplexer) NotifyHostInitialized(ctx context.Context) {
	if len(a.upstreams) == 0 {
		return
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, b := range a.upstreams {
		b := b
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(ctx, a.initTimeout)
			defer cancel()
			req := &rpc.Request{
				JSONRPC: rpc.JSONRPCVersion,
				Method:  "notifications/initialized",
			}
			if _, err := b.Call(callCtx, req); err != nil {
				slog.Warn("notify host initialized upstream failed", "backend_id", b.ID(), "err", err)
			}
			return nil
		})
	}
	_ = g.Wait()
}

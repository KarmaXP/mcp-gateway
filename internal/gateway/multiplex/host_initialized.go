package multiplex

import (
	"context"
	"log/slog"
	"sync"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func (a *Multiplexer) NotifyHostInitialized(ctx context.Context) {
	if len(a.upstreams) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, b := range a.upstreams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, a.initTimeout)
			defer cancel()
			req := &rpc.Request{
				JSONRPC: rpc.JSONRPCVersion,
				Method:  "notifications/initialized",
			}
			if _, err := b.Call(callCtx, req); err != nil {
				slog.Warn("notify host initialized upstream failed", "backend_id", b.ID(), "err", err)
			}
		}()
	}
	wg.Wait()
}

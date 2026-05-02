package httpserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/session"
)

func writeMCPSSEResponseLoop(ctx context.Context, fl http.Flusher, w io.Writer, sess *session.Session, heartbeat time.Duration) {
	if heartbeat <= 0 {
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-sess.Out():
				if !ok {
					return
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", mcpwire.SSEJSONRPCEvent, payload); err != nil {
					return
				}
				fl.Flush()
			}
		}
	}
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		case payload, ok := <-sess.Out():
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", mcpwire.SSEJSONRPCEvent, payload); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

package httpserver

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/session"
)

// writeMCPSSEResponseLoop streams JSON-RPC payloads from the session outbound channel as SSE events.
func writeMCPSSEResponseLoop(ctx context.Context, fl http.Flusher, w io.Writer, sess *session.Session) {
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

// Command smoke_upstream is a minimal MCP HTTP+SSE server for scripts/smoke_test.sh.
package main

import (
	"flag"
	"log"

	"github.com/KarmaXP/mcp-gateway/internal/mcpupstreammock"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:31400", "HTTP listen address")
	flag.Parse()

	if err := mcpupstreammock.Run(mcpupstreammock.Config{
		ListenAddr: *addr,
		ServerName: "smoke-upstream",
		Tools: []mcpupstreammock.Tool{{
			Name:        "echo",
			Description: "smoke echo",
			CallText:    "smoke-ok",
		}},
	}); err != nil {
		log.Fatal(err)
	}
}

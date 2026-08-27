// Command mock_upstream is a configurable MCP HTTP+SSE mock for multi-upstream demos (alpha/beta).
package main

import (
	"flag"
	"log"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/mcpupstreammock"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:3101", "HTTP listen address")
	toolsCSV := flag.String("tools", "echo", "comma-separated native tool names")
	marker := flag.String("marker", "alpha-ok", "tools/call response text for known tools")
	serverName := flag.String("name", "mock-upstream", "MCP serverInfo name")
	flag.Parse()

	names := splitCSV(*toolsCSV)
	tools := make([]mcpupstreammock.Tool, 0, len(names))
	for _, n := range names {
		tools = append(tools, mcpupstreammock.Tool{
			Name:        n,
			Description: n + " mock tool",
			CallText:    *marker,
		})
	}

	if err := mcpupstreammock.Run(mcpupstreammock.Config{
		ListenAddr: *listen,
		ServerName: *serverName,
		Tools:      tools,
	}); err != nil {
		log.Fatal(err)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

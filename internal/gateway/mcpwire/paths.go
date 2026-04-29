// Package mcpwire holds wire-level constants for the MCP HTTP transport (gateway and HTTP upstream clients).
package mcpwire

const (
	PathHealthz = "/healthz"
	PathReadyz  = "/readyz"
	PathMCPSSE  = "/mcp/sse"
	PathMCPRPC  = "/mcp/rpc"

	HeaderMCPSessionID = "Mcp-Session-Id"
)

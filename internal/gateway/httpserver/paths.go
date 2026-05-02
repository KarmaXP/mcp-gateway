package httpserver

import "github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"

// HTTP routes and headers for the MCP host transport (re-exported from mcpwire for callers of this package).
const (
	PathHealthz = mcpwire.PathHealthz
	PathReadyz = mcpwire.PathReadyz
	PathMCPSSE = mcpwire.PathMCPSSE
	PathMCPRPC = mcpwire.PathMCPRPC
	HeaderMCPSessionID = mcpwire.HeaderMCPSessionID
)

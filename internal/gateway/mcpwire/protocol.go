package mcpwire

// MCP wire / handshake strings shared by the gateway, multiplex, and test mocks.
const (
	MCPProtocolVersion = "2024-11-05"
	GatewayClientName = "mcp-gateway"
	GatewayClientVersion = "0.1.0"

	SSEEventLinePrefix = "event:"
	SSEDataLinePrefix = "data:"
	SSEJSONRPCEvent = "jsonrpc"
)

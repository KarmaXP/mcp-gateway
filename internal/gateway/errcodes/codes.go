// Package errcodes defines stable JSON-RPC error codes for the gateway orchestrator.
// Hosts and tests should assert on these values alongside standard JSON-RPC codes where applicable.
package errcodes

// Standard JSON-RPC 2.0 application-defined range is reserved; we use the server-error band for gateway-specific errors.
const (
	// JSON-RPC 2.0 standard
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603 // reserved by spec — do not overload for app logic

	GatewayInternal      = -32000 // backend unreachable, aggregate failure, transport error on fan-out
	HandshakeIncomplete  = -32001 // tools/* before initialize + notifications/initialized
	RequestRejected      = -32002 // middleware or policy rejected the call
	ToolRoutingAmbiguous = -32004 // semantic router: low confidence, tie, or rename disallowed
)

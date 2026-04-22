// Package errcodes lists stable JSON-RPC error codes returned by the multiplexing orchestrator (§3.A.2).
// Hosts and tests should assert on these values. Standard JSON-RPC codes are used where applicable.
package errcodes

// Standard JSON-RPC 2.0 application-defined range is reserved; we use the server-error band for gateway-specific errors.
const (
	// JSON-RPC 2.0 standard
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603 // reserved by spec — do not overload for app logic

	// Gateway application errors (-32000 .. -32099) — thesis / internal contract
	GatewayInternal      = -32000 // backend unreachable, aggregate failure, transport error on fan-out
	HandshakeIncomplete  = -32001 // tools/* before initialize + notifications/initialized
	RequestRejected      = -32002 // middleware / future policy engine (Phase 1: middleware only)
	ToolRoutingAmbiguous = -32004 // semantic router: low confidence, policy block, or rename disallowed (§3.B)
)

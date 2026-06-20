package errcodes

// JSON-RPC 2.0 predefined errors.
const (
	MethodNotFound = -32601
	InvalidParams = -32602
	InternalError = -32603
)

// Gateway JSON-RPC extensions (implementation-defined range per spec).
const (
	GatewayInternal = -32000
	HandshakeIncomplete = -32001
	RequestRejected = -32002
	PermissionDenied = -32003
	ToolRoutingAmbiguous = -32004
	// StrictAggregationFailed when strict aggregation rejects a call with upstream failures.
	StrictAggregationFailed = -32005
)

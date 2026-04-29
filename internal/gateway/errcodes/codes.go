package errcodes

const (
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603

	GatewayInternal      = -32000
	HandshakeIncomplete  = -32001
	RequestRejected      = -32002
	ToolRoutingAmbiguous = -32004
)

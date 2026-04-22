package errcodes

import "testing"

func TestStableCodes(t *testing.T) {
	// Numeric contract for hosts and API docs.
	if MethodNotFound != -32601 || InvalidParams != -32602 {
		t.Fatal("standard JSON-RPC codes drifted")
	}
	if GatewayInternal != -32000 || HandshakeIncomplete != -32001 || RequestRejected != -32002 || ToolRoutingAmbiguous != -32004 {
		t.Fatal("gateway application codes drifted")
	}
}

package mcpwire

// MCP wire / handshake strings shared by the gateway, multiplex, and test mocks.
const (
	MCPProtocolVersion = "2024-11-05"
	GatewayClientName = "mcp-gateway"
	GatewayClientVersion = "0.1.0"

	SSEEventLinePrefix = "event:"
	SSEDataLinePrefix = "data:"
	SSEJSONRPCEvent = "jsonrpc"

	MethodInitialize = "initialize"
	MethodToolsList = "tools/list"
	MethodToolsCall = "tools/call"
	MethodResourcesList = "resources/list"
	MethodResourcesRead = "resources/read"
	MethodPromptsList = "prompts/list"
	MethodPromptsGet = "prompts/get"

	NotificationToolsListChanged = "notifications/tools/list_changed"
	LegacyToolsListChanged = "tools/list_changed"
	NotificationResourcesListChanged = "notifications/resources/list_changed"
	LegacyResourcesListChanged = "resources/list_changed"
	NotificationPromptsListChanged = "notifications/prompts/list_changed"
	LegacyPromptsListChanged = "prompts/list_changed"
)

func IsToolsListChangedNotification(method string) bool {
	return method == NotificationToolsListChanged || method == LegacyToolsListChanged
}

func IsResourcesListChangedNotification(method string) bool {
	return method == NotificationResourcesListChanged || method == LegacyResourcesListChanged
}

func IsPromptsListChangedNotification(method string) bool {
	return method == NotificationPromptsListChanged || method == LegacyPromptsListChanged
}

func IsCatalogListChangedNotification(method string) bool {
	return IsToolsListChangedNotification(method) ||
		IsResourcesListChangedNotification(method) ||
		IsPromptsListChangedNotification(method)
}

func IsReplayableMethod(method string) bool {
	switch method {
	case MethodInitialize,
		MethodToolsList,
		MethodResourcesList,
		MethodResourcesRead,
		MethodPromptsList,
		MethodPromptsGet:
		return true
	default:
		return false
	}
}

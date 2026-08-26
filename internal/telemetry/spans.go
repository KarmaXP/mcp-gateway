package telemetry

// OTLP span names for the gateway (stable identifiers for dashboards and sampling rules).
const (
	SpanMCPHostRequest = "mcp.host.request"
	SpanSecurityAuthn = "mcp.security.authn"
	SpanSecurityAuthz = "mcp.security.authz"
	SpanValidateJSONSchema = "mcp.validate.json_schema"
	SpanMultiplexInit = "mcp.multiplex.initialize"
	SpanMultiplexToolsList = "mcp.multiplex.tools_list"
	SpanMultiplexResourcesList = "mcp.multiplex.resources_list"
	SpanMultiplexResourcesRead = "mcp.multiplex.resources_read"
	SpanMultiplexPromptsList = "mcp.multiplex.prompts_list"
	SpanMultiplexPromptsGet = "mcp.multiplex.prompts_get"
	SpanSemanticRouter = "mcp.router.semantic"
	// SpanBackendCall keeps the "backend" spelling: renaming it breaks deployed dashboards.
	SpanBackendCall = "mcp.backend.call"
)

package telemetry

// OTLP span names for the gateway (stable identifiers for dashboards and sampling rules).
const (
	SpanMCPHostRequest     = "mcp.host.request"
	SpanSecurityAuthz      = "mcp.security.authz"
	SpanValidateJSONSchema = "mcp.validate.json_schema"
	SpanMultiplexInit      = "mcp.multiplex.initialize"
	SpanMultiplexToolsList = "mcp.multiplex.tools_list"
	SpanSemanticRouter     = "mcp.router.semantic"
	SpanBackendCall        = "mcp.backend.call"
)

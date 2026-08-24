package telemetry

import (
	"encoding/json"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

// Low-cardinality span attributes. Do not put unbounded user text here.
const (
	AttrMCPMethod = "mcp.method"
	AttrMCPJSONRPCID = "mcp.jsonrpc.id"
	AttrMCPSessionID = "mcp.session.id"
	AttrMCPBackendID = "mcp.backend.id"
	AttrMCPToolName = "mcp.tool.name"
	AttrMCPAgentTokensUsed = "mcp.agent.tokens_used"
)

// AttrJSONRPCID truncates raw JSON-RPC id for span export (bounded size).
func AttrJSONRPCID(raw json.RawMessage) attribute.KeyValue {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return attribute.String(AttrMCPJSONRPCID, "")
	}
	maxBytes := defaults.MaxOTelSpanAttributeBytes
	if len(s) > maxBytes {
		s = s[:maxBytes] + "..."
	}
	return attribute.String(AttrMCPJSONRPCID, s)
}

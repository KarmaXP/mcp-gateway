package auth

import (
	"encoding/json"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// TokenClaims is the JWT claim set used for MCP authorization (AuthN + inputs to policy).
type TokenClaims struct {
	jwt.RegisteredClaims
	McpTools             []string        `json:"mcp_tools,omitempty"`
	McpToolGroups        []string        `json:"mcp_tool_groups,omitempty"`
	TenantID             string          `json:"tenant_id,omitempty"`
	AuthorizationDetails json.RawMessage `json:"authorization_details,omitempty"`
}

// Subject returns the JWT sub claim (for audit hashing only; do not log raw in production paths).
func (c *TokenClaims) Subject() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.RegisteredClaims.Subject)
}

// NormalizedMcpTools returns trimmed, deduped mcp_tools claim entries.
func (c *TokenClaims) NormalizedMcpTools() []string {
	if c == nil {
		return nil
	}
	return normalizeMcpToolNames(c.McpTools)
}

// NormalizedToolGroups returns trimmed mcp_tool_groups claim entries.
func (c *TokenClaims) NormalizedToolGroups() []string {
	if c == nil || len(c.McpToolGroups) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.McpToolGroups))
	for _, s := range c.McpToolGroups {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RawAuthorizationDetails returns RFC 9396 authorization_details JSON (RAR).
func (c *TokenClaims) RawAuthorizationDetails() json.RawMessage {
	if c == nil {
		return nil
	}
	return c.AuthorizationDetails
}

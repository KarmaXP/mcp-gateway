package auth

import (
	"encoding/json"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// MCP-facing JWT claims (AuthN + inputs to policy.Engine).
type TokenClaims struct {
	jwt.RegisteredClaims
	McpTools             []string        `json:"mcp_tools,omitempty"`
	McpToolGroups        []string        `json:"mcp_tool_groups,omitempty"`
	TenantID             string          `json:"tenant_id,omitempty"`
	AuthorizationDetails json.RawMessage `json:"authorization_details,omitempty"`
}

func (c *TokenClaims) Subject() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.RegisteredClaims.Subject)
}

func (c *TokenClaims) NormalizedMcpTools() []string {
	if c == nil {
		return nil
	}
	return normalizeMcpToolNames(c.McpTools)
}

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

// RFC 9396 authorization_details (RAR) as raw JSON.
func (c *TokenClaims) RawAuthorizationDetails() json.RawMessage {
	if c == nil {
		return nil
	}
	return c.AuthorizationDetails
}

package policy

import "encoding/json"

// ClaimsInput is the token-derived view used by the policy engine (implemented by auth.TokenClaims).
// Kept in this package so policy does not import auth (avoids an import cycle with auth middleware).
type ClaimsInput interface {
	Subject() string
	NormalizedMcpTools() []string
	NormalizedToolGroups() []string
	RawAuthorizationDetails() json.RawMessage
}

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func TestHTTPMiddlewarePolicyIntersectsRARAndMcpTools(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	ad, _ := json.Marshal([]map[string]any{
		{"type": "mcp_tool", "tool_pattern": "alpha__*"},
	})
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "iss",
			Audience:  jwt.ClaimStrings{"aud"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
		McpTools:             []string{"alpha__echo", "beta__nope"},
		AuthorizationDetails: ad,
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(priv)
	require.NoError(t, err)

	eng := policy.NewEngine(config.PolicySettings{Version: "v-test"})
	holder := policy.NewHolder(eng)
	var got []string
	h := HTTPMiddleware(cfg, v, holder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = hostctx.AllowedToolNamesFromContext(r.Context())
		require.Equal(t, "user-1", hostctx.SubjectIDFromContext(r.Context()))
		require.Equal(t, "v-test", hostctx.PolicyVersionFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, res.Body.Close())
	require.Equal(t, []string{"alpha__echo"}, got)
}

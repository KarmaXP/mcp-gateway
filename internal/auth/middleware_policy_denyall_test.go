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

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func TestHTTPMiddlewareEmptyIntersectionDenyAll(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	ad, _ := json.Marshal([]map[string]any{
		{"type": "mcp_tool", "tool_name": "rar__only"},
	})
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "iss",
			Audience:  jwt.ClaimStrings{"aud"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
		McpTools:             []string{"jwt__only"},
		AuthorizationDetails: ad,
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(priv)
	require.NoError(t, err)

	eng := policy.NewEngine(policy.EngineInput{Version: "v-test"})
	holder := policy.NewHolder(eng)
	var mode hostctx.AllowListMode
	var got []string
	h := HTTPMiddleware(cfg, v, holder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode, got = hostctx.AllowListModeFromContext(r.Context())
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
	require.Equal(t, hostctx.AllowListDenyAll, mode)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestHTTPMiddlewareTokenWithoutToolClaimsIsDenyAll(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	sign := func(tools []string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, TokenClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-1",
				Issuer:    "iss",
				Audience:  jwt.ClaimStrings{"aud"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			},
			McpTools: tools,
		})
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(priv)
		require.NoError(t, err)
		return s
	}

	tests := []struct {
		name      string
		tools     []string
		wantMode  hostctx.AllowListMode
		wantNames []string
	}{
		{
			name:      "no tool claims at all",
			wantMode:  hostctx.AllowListDenyAll,
			wantNames: []string{},
		},
		{
			name:      "the full catalog is asked for explicitly",
			tools:     []string{"*"},
			wantMode:  hostctx.AllowListRestricted,
			wantNames: []string{"*"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mode hostctx.AllowListMode
			var names []string
			h := HTTPMiddleware(cfg, v, policy.NewHolder(policy.NewEngine(policy.EngineInput{})))(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mode, names = hostctx.AllowListModeFromContext(r.Context())
					w.WriteHeader(http.StatusOK)
				}))
			ts := httptest.NewServer(h)
			defer ts.Close()

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
			req.Header.Set("Authorization", "Bearer "+sign(tc.tools))
			res, err := ts.Client().Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, res.StatusCode)
			require.NoError(t, res.Body.Close())
			require.Equal(t, tc.wantMode, mode)
			require.Equal(t, tc.wantNames, names)
		})
	}
}

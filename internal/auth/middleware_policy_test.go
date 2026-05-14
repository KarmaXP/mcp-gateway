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

func TestHTTPMiddlewarePolicyVersionAfterHolderReload(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "iss",
			Audience:  jwt.ClaimStrings{"aud"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(priv)
	require.NoError(t, err)

	holder := policy.NewHolder(policy.NewEngine(config.PolicySettings{Version: "before"}))
	var versions []string
	h := HTTPMiddleware(cfg, v, holder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versions = append(versions, hostctx.PolicyVersionFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req1, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
	req1.Header.Set("Authorization", "Bearer "+s)
	res1, err := ts.Client().Do(req1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res1.StatusCode)
	require.NoError(t, res1.Body.Close())

	policy.ReloadEngine(holder, config.GatewayConfig{
		Policy: config.PolicySettings{Version: "after"},
	})

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
	req2.Header.Set("Authorization", "Bearer "+s)
	res2, err := ts.Client().Do(req2)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res2.StatusCode)
	require.NoError(t, res2.Body.Close())

	require.Equal(t, []string{"before", "after"}, versions)
}

func TestHTTPMiddlewarePolicyAllowOnEvalFailureControlsRARDegradation(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	malformedRAR, err := json.Marshal([]map[string]any{
		{
			"type":         "mcp_tool",
			"tool_name":    "alpha__echo",
			"tool_pattern": "alpha__*",
		},
	})
	require.NoError(t, err)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "iss",
			Audience:  jwt.ClaimStrings{"aud"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
		McpTools:             []string{"alpha__echo", "beta__view"},
		AuthorizationDetails: malformedRAR,
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(priv)
	require.NoError(t, err)

	tests := []struct {
		name               string
		allowOnEvalFailure bool
		wantStatus         int
		wantAllowedTools   []string
	}{
		{
			name:               "fail closed when disabled",
			allowOnEvalFailure: false,
			wantStatus:         http.StatusUnauthorized,
		},
		{
			name:               "degrade to JWT tools when enabled",
			allowOnEvalFailure: true,
			wantStatus:         http.StatusOK,
			wantAllowedTools:   []string{"alpha__echo", "beta__view"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			holder := policy.NewHolder(policy.NewEngine(config.PolicySettings{
				Version:            "v-test",
				AllowOnEvalFailure: tc.allowOnEvalFailure,
			}))
			var got []string
			h := HTTPMiddleware(cfg, v, holder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = hostctx.AllowedToolNamesFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))
			ts := httptest.NewServer(h)
			defer ts.Close()

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
			req.Header.Set("Authorization", "Bearer "+s)
			res, err := ts.Client().Do(req)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, res.StatusCode)
			require.NoError(t, res.Body.Close())
			require.Equal(t, tc.wantAllowedTools, got)
		})
	}
}

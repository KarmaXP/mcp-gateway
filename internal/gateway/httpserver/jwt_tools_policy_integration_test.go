//go:build integration

package httpserver_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/orchestrator"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func testRSAKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return priv, string(pubPEM)
}

func signTokenClaims(t *testing.T, priv *rsa.PrivateKey, claims *auth.TokenClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(priv)
	require.NoError(t, err)
	return s
}

// TestIntegrationJWTPermissionDeniedSkipsBackend verifies a signed JWT with a narrow mcp_tools allow list:
// tools/call for a disallowed namespaced tool returns JSON-RPC -32003 on SSE and does not forward to the mock upstream.
func TestIntegrationJWTPermissionDeniedSkipsBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	priv, pubPEM := testRSAKeyPair(t)
	const iss = "https://p1.integration.test"
	const aud = "mcp-gateway-p1"

	claims := &auth.TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    iss,
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour)),
		},
		McpTools: []string{"alpha__echo"},
	}
	token := signTokenClaims(t, priv, claims)

	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "other"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	authCfg := auth.JWTAuthConfig{
		Mode:         "jwt",
		Issuer:       iss,
		Audience:     aud,
		PublicKeyPEM: pubPEM,
	}
	v, err := auth.NewValidator(authCfg)
	require.NoError(t, err)

	opts := orchestrator.HTTPServerOptions("mcp-gateway-p1-it", authCfg, v, policy.NewHolder(policy.NewEngine(config.PolicySettings{})), ratelimit.Config{})
	srv := httpserver.New(agg, "", opts...)
	ts := httptest.NewServer(srv.AsHandler())
	defer ts.Close()

	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()
	client := ts.Client()

	sseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+httpserver.PathMCPSSE, nil)
	require.NoError(t, err)
	sseReq.Header.Set("Authorization", "Bearer "+token)
	sseResp, err := client.Do(sseReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, sseResp.StatusCode)
	sid := sseResp.Header.Get(httpserver.HeaderMCPSessionID)
	require.NotEmpty(t, sid)

	dataCh := make(chan string, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer sseResp.Body.Close()
		br := bufio.NewReader(sseResp.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				dataCh <- strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
	}()

	post := func(jsonBody string) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+httpserver.PathMCPRPC, strings.NewReader(jsonBody))
		require.NoError(t, err)
		req.Header.Set(httpserver.HeaderMCPSessionID, sid)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		pr, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, pr.StatusCode)
		_, _ = io.Copy(io.Discard, pr.Body)
		require.NoError(t, pr.Body.Close())
	}

	post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	select {
	case d := <-dataCh:
		var jr rpc.Response
		require.NoError(t, json.Unmarshal([]byte(d), &jr))
		require.Nil(t, jr.Error)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout initialize")
	}

	post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	require.Equal(t, uint64(0), b1.ToolsCallInvocationCount())

	post(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"alpha__other","arguments":{}}}`)
	select {
	case d := <-dataCh:
		var jr rpc.Response
		require.NoError(t, json.Unmarshal([]byte(d), &jr))
		require.NotNil(t, jr.Error)
		require.Equal(t, errcodes.PermissionDenied, jr.Error.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout tools/call deny")
	}

	require.Equal(t, uint64(0), b1.ToolsCallInvocationCount(), "backend must not run tools/call when JWT allow-list denies")

	cancelSSE()
	wg.Wait()
}

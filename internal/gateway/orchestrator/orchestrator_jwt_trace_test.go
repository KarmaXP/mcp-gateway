package orchestrator

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestHTTPServerOptionsJWTAndOTelProduceSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	cfg := auth.JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: pubPEM}
	v, err := auth.NewValidator(cfg)
	require.NoError(t, err)

	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	agg, err := multiplex.New([]backend.Upstream{b1, b2}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	opts := HTTPServerOptions("mcp-gateway-test", cfg, v, nil, ratelimit.Config{})
	srv := httpserver.New(agg, "", opts...)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    "iss",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	})
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(priv)
	require.NoError(t, err)
	authz := "Bearer " + signed

	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()
	client := ts.Client()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+httpserver.PathMCPSSE, nil)
	sseReq.Header.Set("Authorization", authz)
	sseResp, err := client.Do(sseReq)
	require.NoError(t, err)
	sid := sseResp.Header.Get(httpserver.HeaderMCPSessionID)
	require.NotEmpty(t, sid)

	dataCh := make(chan string, 16)
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
		req, _ := http.NewRequest(http.MethodPost, ts.URL+httpserver.PathMCPRPC, strings.NewReader(jsonBody))
		req.Header.Set(httpserver.HeaderMCPSessionID, sid)
		req.Header.Set("Authorization", authz)
		req.Header.Set("Content-Type", "application/json")
		pr, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, pr.StatusCode)
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
	post(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)

	select {
	case d := <-dataCh:
		var jr rpc.Response
		require.NoError(t, json.Unmarshal([]byte(d), &jr))
		require.Nil(t, jr.Error)
		require.Contains(t, string(jr.Result), "alpha__echo")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout tools/list")
	}

	cancelSSE()
	wg.Wait()

	spans := rec.Ended()
	require.NotEmpty(t, spans)
	for _, s := range spans {
		require.True(t, s.SpanContext().TraceID().IsValid(), "span %s missing trace", s.Name())
	}
}

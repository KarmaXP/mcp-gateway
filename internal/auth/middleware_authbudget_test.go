package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestFailedAuthenticationsStopReachingTheValidator(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	limiter := ratelimit.New(t.Context(), ratelimit.Config{})
	h := HTTPMiddleware(cfg, v, nil, limiter)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler reached with an invalid token")
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	bad := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    "iss",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	})
	bad.Header["kid"] = "k1"
	signed, err := bad.SignedString(otherKey)
	require.NoError(t, err)

	var unauthorized, throttled int
	for range defaults.AuthFailureBudgetBurst + 5 {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
		req.Header.Set("Authorization", "Bearer "+signed)
		res, err := ts.Client().Do(req)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		switch res.StatusCode {
		case http.StatusUnauthorized:
			unauthorized++
		case http.StatusTooManyRequests:
			throttled++
		default:
			t.Fatalf("unexpected status %d", res.StatusCode)
		}
	}
	require.Equal(t, defaults.AuthFailureBudgetBurst, unauthorized, "the budget is spent by failures only")
	require.Positive(t, throttled, "an unbounded client can force unlimited signature verification")
}

func TestFailedAuthenticationsStopFloodingTheJWKSEndpoint(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var fetches atomic.Int64
	srv := jwksServer(t, priv, "signing-1", &fetches)

	cfg := JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", JWKSURL: srv.URL}
	v, err := NewValidator(cfg)
	require.NoError(t, err)
	afterWarmup := fetches.Load()

	limiter := ratelimit.New(t.Context(), ratelimit.Config{})
	h := HTTPMiddleware(cfg, v, nil, limiter)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler reached with an unknown kid")
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	unknownKid := signedToken(t, priv, "rotated-away", nil)
	const attempts = 40
	for range attempts {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/rpc", nil)
		req.Header.Set("Authorization", "Bearer "+unknownKid)
		res, err := ts.Client().Do(req)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}

	forced := fetches.Load() - afterWarmup
	require.LessOrEqual(t, forced, int64(defaults.AuthFailureBudgetBurst),
		"an unknown kid refetches the JWKS, so %d attempts must not mean %d fetches against the IdP", attempts, forced)
}

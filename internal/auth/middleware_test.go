package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func rsaPublicPEM(t *testing.T, pub *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	b := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return string(b)
}

func TestHTTPMiddlewareJWTAcceptsValidToken(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := Config{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    "iss",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	})
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(priv)
	require.NoError(t, err)

	var hit bool
	h := HTTPMiddleware(cfg, v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/sse", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, res.Body.Close())
	require.True(t, hit)
}

func TestHTTPMiddlewareRejectsMissingBearer(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := Config{Mode: "jwt", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	h := HTTPMiddleware(cfg, v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("should not reach handler")
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/mcp/sse")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddlewareNoneModePassesThrough(t *testing.T) {
	cfg := Config{Mode: "none"}
	h := HTTPMiddleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/mcp/sse")
	require.NoError(t, err)
	require.Equal(t, http.StatusTeapot, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddlewareSkipsConfiguredPrefixes(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := Config{
		Mode:             "jwt",
		PublicKeyPEM:     rsaPublicPEM(t, &priv.PublicKey),
		SkipPathPrefixes: []string{"/healthz", "/metrics"},
	}
	v, err := NewValidator(cfg)
	require.NoError(t, err)
	h := HTTPMiddleware(cfg, v)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/healthz/live")
	require.NoError(t, err)
	require.Equal(t, http.StatusTeapot, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddlewareRejectsNonBearerScheme(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := Config{Mode: "jwt", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)
	h := HTTPMiddleware(cfg, v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler reached")
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/sse", nil)
	req.Header.Set("Authorization", "Basic dGVzdA==")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddlewareRejectsEmptyBearerToken(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := Config{Mode: "jwt", PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey)}
	v, err := NewValidator(cfg)
	require.NoError(t, err)
	h := HTTPMiddleware(cfg, v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler reached")
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/sse", nil)
	req.Header.Set("Authorization", "Bearer   ")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddlewareRejectsNilValidatorInJWTMode(t *testing.T) {
	cfg := Config{Mode: "jwt"}
	h := HTTPMiddleware(cfg, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler reached")
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp/sse", nil)
	req.Header.Set("Authorization", "Bearer x.y.z")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

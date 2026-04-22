package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/require"
)

func TestValidator_JWKSRS256(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	key, err := jwk.FromRaw(priv.PublicKey)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, "signing-1"))

	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))
	jwksBody, err := json.Marshal(set)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksBody)
	}))
	defer srv.Close()

	cfg := Config{
		Mode:     "jwt",
		Issuer:   "https://issuer.example",
		Audience: "mcp-aud",
		JWKSURL:  srv.URL,
	}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    cfg.Issuer,
		Audience:  jwt.ClaimStrings{cfg.Audience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	})
	tok.Header["kid"] = "signing-1"
	signed, err := tok.SignedString(priv)
	require.NoError(t, err)

	require.NoError(t, v.Validate(context.Background(), signed))
}

func TestValidator_JWKSMissingKid(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	key, err := jwk.FromRaw(priv.PublicKey)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, "k1"))
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))
	jwksBody, err := json.Marshal(set)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jwksBody)
	}))
	defer srv.Close()

	cfg := Config{Mode: "jwt", Issuer: "iss", Audience: "aud", JWKSURL: srv.URL}
	v, err := NewValidator(cfg)
	require.NoError(t, err)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    "iss",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	})
	// kid omitted on purpose
	signed, err := tok.SignedString(priv)
	require.NoError(t, err)

	require.Error(t, v.Validate(context.Background(), signed))
}

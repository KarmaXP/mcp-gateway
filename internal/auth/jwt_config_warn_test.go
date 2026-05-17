package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatorWarnsWhenPEMAndJWKSConfigured(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	v, err := NewValidator(JWTAuthConfig{
		Mode:         "jwt",
		Issuer:       "iss",
		Audience:     "aud",
		PublicKeyPEM: pubPEM,
		JWKSURL:      srv.URL,
	})
	require.NoError(t, err)
	require.NotNil(t, v)
}

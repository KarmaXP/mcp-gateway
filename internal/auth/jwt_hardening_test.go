package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/require"
)

func jwksServer(t *testing.T, priv *rsa.PrivateKey, kid string, fetches *atomic.Int64) *httptest.Server {
	t.Helper()
	key, err := jwk.FromRaw(priv.PublicKey)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, kid))
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))
	body, err := json.Marshal(set)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fetches != nil {
			fetches.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func signedToken(t *testing.T, priv *rsa.PrivateKey, kid string, mod func(*jwt.RegisteredClaims)) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Issuer:    "iss",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	if mod != nil {
		mod(&claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(priv)
	require.NoError(t, err)
	return s
}

func TestNewValidatorRejectsPlaintextJWKSExceptOnLoopback(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	loopback := jwksServer(t, priv, "k1", nil)

	tests := []struct {
		name    string
		jwksURL string
		wantErr string
	}{
		{name: "loopback http is allowed", jwksURL: loopback.URL},
		{name: "remote http is refused", jwksURL: "http://idp.example/jwks.json", wantErr: `must use https, got scheme "http"`},
		{name: "no scheme is refused", jwksURL: "idp.example/jwks.json", wantErr: `must use https, got scheme ""`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewValidator(JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", JWKSURL: tc.jwksURL})
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err, "a plaintext JWKS lets a MITM supply the signing keys")
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidatorCachesJWKSWhenNoTTLWasConfigured(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var fetches atomic.Int64
	srv := jwksServer(t, priv, "k1", &fetches)

	v, err := NewValidator(JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", JWKSURL: srv.URL})
	require.NoError(t, err)
	afterWarmup := fetches.Load()

	token := signedToken(t, priv, "k1", nil)
	for range 3 {
		require.NoError(t, v.Validate(context.Background(), token))
	}
	require.Equal(t, afterWarmup, fetches.Load(), "a zero TTL refetches the JWKS on every request")
}

func TestValidatorDoesNotFetchJWKSForATokenWithoutKid(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var fetches atomic.Int64
	srv := jwksServer(t, priv, "k1", &fetches)

	v, err := NewValidator(JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", JWKSURL: srv.URL, JWKSCacheTTL: time.Nanosecond})
	require.NoError(t, err)
	afterWarmup := fetches.Load()

	err = v.Validate(context.Background(), signedToken(t, priv, "", nil))
	require.ErrorContains(t, err, "missing kid")
	require.Equal(t, afterWarmup, fetches.Load(), "a token with no kid must not cost a network fetch")
}

func TestValidatorToleratesIdPClockSkew(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	v, err := NewValidator(JWTAuthConfig{
		Mode:         "jwt",
		Issuer:       "iss",
		Audience:     "aud",
		PublicKeyPEM: rsaPublicPEM(t, &priv.PublicKey),
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		skew    time.Duration
		wantErr bool
	}{
		{name: "expired 30s ago is within leeway", skew: -30 * time.Second},
		{name: "issued 30s in the future is within leeway", skew: 30 * time.Second},
		{name: "expired 5m ago is not", skew: -5 * time.Minute, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := signedToken(t, priv, "k1", func(c *jwt.RegisteredClaims) {
				if tc.skew < 0 {
					c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(tc.skew))
					return
				}
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(tc.skew))
			})
			err := v.Validate(context.Background(), token)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err, "a few seconds of IdP drift must not produce a 401")
		})
	}
}

func TestValidatorRejectsUndersizedSigningKeys(t *testing.T) {
	small, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)

	t.Run("static PEM", func(t *testing.T) {
		_, err := NewValidator(JWTAuthConfig{
			Mode:         "jwt",
			Issuer:       "iss",
			Audience:     "aud",
			PublicKeyPEM: rsaPublicPEM(t, &small.PublicKey),
		})
		require.ErrorContains(t, err, "RSA key is 1024 bits, minimum is 2048")
	})

	t.Run("from JWKS", func(t *testing.T) {
		srv := jwksServer(t, small, "k1", nil)
		v, err := NewValidator(JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", JWKSURL: srv.URL})
		require.NoError(t, err, "the warmup fetch does not inspect keys")
		err = v.Validate(context.Background(), signedToken(t, small, "k1", nil))
		require.ErrorContains(t, err, "RSA key is 1024 bits, minimum is 2048")
	})
}

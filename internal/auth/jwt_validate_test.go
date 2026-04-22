package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func rsaPubPEM(t *testing.T, pub *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestValidator_TableDriven(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pubPEM := rsaPubPEM(t, &priv.PublicKey)

	baseCfg := Config{
		Mode:         "jwt",
		Issuer:       "https://issuer.example",
		Audience:     "mcp-aud",
		PublicKeyPEM: pubPEM,
	}
	v, err := NewValidator(baseCfg)
	require.NoError(t, err)
	ctx := context.Background()

	sign := func(mod func(*jwt.RegisteredClaims)) string {
		cl := jwt.RegisteredClaims{
			Issuer:    baseCfg.Issuer,
			Audience:  jwt.ClaimStrings{baseCfg.Audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		}
		if mod != nil {
			mod(&cl)
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, cl)
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(priv)
		require.NoError(t, err)
		return s
	}

	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"valid", sign(nil), false},
		{"expired", sign(func(c *jwt.RegisteredClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
		}), true},
		{"wrong_aud", sign(func(c *jwt.RegisteredClaims) {
			c.Audience = jwt.ClaimStrings{"other"}
		}), true},
		{"wrong_iss", sign(func(c *jwt.RegisteredClaims) {
			c.Issuer = "evil"
		}), true},
		{"malformed", "not-a-jwt", true},
		{"wrong_sig", func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
				Issuer:    baseCfg.Issuer,
				Audience:  jwt.ClaimStrings{baseCfg.Audience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			})
			tok.Header["kid"] = "k1"
			s, err := tok.SignedString(wrongKey)
			require.NoError(t, err)
			return s
		}(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(ctx, tc.token)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

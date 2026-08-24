package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatorUnknownMode(t *testing.T) {
	_, err := NewValidator(JWTAuthConfig{Mode: "oauth"})
	require.Error(t, err)
}

func TestNewValidatorInvalidPEM(t *testing.T) {
	_, err := NewValidator(JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud", PublicKeyPEM: "not pem"})
	require.Error(t, err)
}

func TestNewValidatorJWTRequiresKeyMaterial(t *testing.T) {
	_, err := NewValidator(JWTAuthConfig{Mode: "jwt", Issuer: "iss", Audience: "aud"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "JWKS URL or JWT_PUBLIC_KEY_PEM required")
}

func TestNewValidatorNoneReturnsNil(t *testing.T) {
	v, err := NewValidator(JWTAuthConfig{Mode: "none"})
	require.NoError(t, err)
	require.Nil(t, v)
	v2, err := NewValidator(JWTAuthConfig{Mode: ""})
	require.NoError(t, err)
	require.Nil(t, v2)
}

func TestNewValidatorJWTRequiresIssuerAndAudience(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pubPEM := rsaPublicPEM(t, &priv.PublicKey)

	tests := []struct {
		name    string
		cfg     JWTAuthConfig
		wantErr string
	}{
		{
			name:    "neither is set",
			cfg:     JWTAuthConfig{Mode: "jwt", PublicKeyPEM: pubPEM},
			wantErr: "JWT_ISS and JWT_AUD required",
		},
		{
			name:    "issuer missing",
			cfg:     JWTAuthConfig{Mode: "jwt", Audience: "aud", PublicKeyPEM: pubPEM},
			wantErr: "JWT_ISS required",
		},
		{
			name:    "audience missing",
			cfg:     JWTAuthConfig{Mode: "jwt", Issuer: "iss", PublicKeyPEM: pubPEM},
			wantErr: "JWT_AUD required",
		},
		{
			name:    "blank counts as missing",
			cfg:     JWTAuthConfig{Mode: "jwt", Issuer: "  ", Audience: "\t", PublicKeyPEM: pubPEM},
			wantErr: "JWT_ISS and JWT_AUD required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewValidator(tc.cfg)
			require.Error(t, err, "without both settings any token the key verifies would authenticate")
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

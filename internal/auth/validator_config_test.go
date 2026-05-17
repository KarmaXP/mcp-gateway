package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatorUnknownMode(t *testing.T) {
	_, err := NewValidator(JWTAuthConfig{Mode: "oauth"})
	require.Error(t, err)
}

func TestNewValidatorInvalidPEM(t *testing.T) {
	_, err := NewValidator(JWTAuthConfig{Mode: "jwt", PublicKeyPEM: "not pem"})
	require.Error(t, err)
}

func TestNewValidatorJWTRequiresKeyMaterial(t *testing.T) {
	_, err := NewValidator(JWTAuthConfig{Mode: "jwt"})
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

package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatorUnknownMode(t *testing.T) {
	_, err := NewValidator(Config{Mode: "oauth"})
	require.Error(t, err)
}

func TestNewValidatorInvalidPEM(t *testing.T) {
	_, err := NewValidator(Config{Mode: "jwt", PublicKeyPEM: "not pem"})
	require.Error(t, err)
}

func TestNewValidatorNoneReturnsNil(t *testing.T) {
	v, err := NewValidator(Config{Mode: "none"})
	require.NoError(t, err)
	require.Nil(t, v)
	v2, err := NewValidator(Config{Mode: ""})
	require.NoError(t, err)
	require.Nil(t, v2)
}

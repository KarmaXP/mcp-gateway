package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyFileErrorOnlyMattersWhenJWTIsOn(t *testing.T) {
	t.Setenv("JWT_PUBLIC_KEY_FILE", "/nonexistent/key.pem")

	t.Setenv("AUTH_MODE", "none")
	cfg, err := JWTAuthFromEnvironment()
	require.NoError(t, err, "a key the gateway will not read must not stop it from starting")
	require.Empty(t, cfg.PublicKeyPEM)

	t.Setenv("AUTH_MODE", "jwt")
	_, err = JWTAuthFromEnvironment()
	require.ErrorContains(t, err, "/nonexistent/key.pem",
		"with jwt on, an unreadable key must name the file rather than yield an empty one")
}

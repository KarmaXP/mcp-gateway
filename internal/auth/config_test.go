package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	t.Setenv("JWT_ISS", "https://issuer")
	t.Setenv("JWT_AUD", "audience")
	t.Setenv("JWT_JWKS_URL", "https://issuer/jwks")
	t.Setenv("JWT_PUBLIC_KEY_PEM", "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----")
	t.Setenv("JWT_JWKS_CACHE_TTL", "2m")

	c := ConfigFromEnv()
	require.Equal(t, "jwt", c.Mode)
	require.Equal(t, "https://issuer", c.Issuer)
	require.Equal(t, "audience", c.Audience)
	require.Equal(t, "https://issuer/jwks", c.JWKSURL)
	require.Contains(t, c.PublicKeyPEM, "BEGIN PUBLIC KEY")
	require.Equal(t, 2*time.Minute, c.JWKSCacheTTL)
}

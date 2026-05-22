package auth

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJWTAuthFromEnvironment(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	t.Setenv("JWT_ISS", "https://issuer")
	t.Setenv("JWT_AUD", "audience")
	t.Setenv("JWT_JWKS_URL", "https://issuer/jwks")
	t.Setenv("JWT_PUBLIC_KEY_PEM", "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----")
	t.Setenv("JWT_JWKS_CACHE_TTL", "2m")

	c := JWTAuthFromEnvironment()
	require.Equal(t, "jwt", c.Mode)
	require.Equal(t, "https://issuer", c.Issuer)
	require.Equal(t, "audience", c.Audience)
	require.Equal(t, "https://issuer/jwks", c.JWKSURL)
	require.Contains(t, c.PublicKeyPEM, "BEGIN PUBLIC KEY")
	require.Equal(t, 2*time.Minute, c.JWKSCacheTTL)
}

func TestJWTAuthFromEnvironmentPublicKeyFile(t *testing.T) {
	t.Setenv("JWT_PUBLIC_KEY_PEM", "")
	t.Setenv("JWT_PUBLIC_KEY_FILE", t.TempDir()+"/jwt.pub.pem")
	require.NoError(t, os.WriteFile(os.Getenv("JWT_PUBLIC_KEY_FILE"), []byte(testRSAPublicPEM), 0o600))

	c := JWTAuthFromEnvironment()
	require.Contains(t, c.PublicKeyPEM, "BEGIN PUBLIC KEY")
}

const testRSAPublicPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv6EVs4HOct3LXBvV3x+s
/gQ0dOkAMGSzB5YFBm5OC22r7Jk029THAEl8gu4D2U9yw3BGj0sx8B9ZlgvfmeNk
/9B+ovte4ui4FRRmbj3unzC1cRmS1zvHs9kRCVA56ZEj39FsoOlDaVUmE85rIhWv
U56xcxzcpmZ4nLd3XcKcaAXTSMWHp2m5uo2qtwQnbauU7bqOAsJjtj10r1iHqTyQ
RPajf94cFFuuc1Vvyb7IGjTPT+pC+ybMvB8uS9CYNDXtOpjc1Iw8/JUkfOQRERuc
bKQuWE32FdfdUjJ9QQO4BI4soCwjBCkUMVsJlekahR5f/l5KrOxUXQqtQn0gsR4F
+QIDAQAB
-----END PUBLIC KEY-----`

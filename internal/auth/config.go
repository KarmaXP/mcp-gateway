package auth

import (
	"os"
	"strings"
	"time"
)

// Config drives HTTP bearer validation (SEC1 — stateless signature check).
type Config struct {
	Mode string // "none" (default), "jwt"

	Issuer   string
	Audience string

	// JWKSURL is fetched with a TTL cache (SEC6).
	JWKSURL string
	// PublicKeyPEM optional RS256 public key (PEM) for local dev when JWKSURL is empty.
	PublicKeyPEM string

	JWKSCacheTTL time.Duration

	// SkipPathPrefixes are not challenged (e.g. health probes).
	SkipPathPrefixes []string
}

// ConfigFromEnv reads AUTH_MODE, JWT_ISS, JWT_AUD, JWT_JWKS_URL, JWT_PUBLIC_KEY_PEM.
func ConfigFromEnv() Config {
	ttl := 5 * time.Minute
	if v := os.Getenv("JWT_JWKS_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	return Config{
		Mode:             strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE"))),
		Issuer:           strings.TrimSpace(os.Getenv("JWT_ISS")),
		Audience:         strings.TrimSpace(os.Getenv("JWT_AUD")),
		JWKSURL:          strings.TrimSpace(os.Getenv("JWT_JWKS_URL")),
		PublicKeyPEM:     strings.TrimSpace(os.Getenv("JWT_PUBLIC_KEY_PEM")),
		JWKSCacheTTL:     ttl,
		SkipPathPrefixes: []string{"/healthz", "/readyz"},
	}
}

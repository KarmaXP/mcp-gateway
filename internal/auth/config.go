package auth

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Mode string

	Issuer   string
	Audience string

	JWKSURL      string
	PublicKeyPEM string

	JWKSCacheTTL time.Duration

	SkipPathPrefixes []string
}

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

package auth

import (
	"os"
	"strings"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
)

type JWTAuthConfig struct {
	Mode string

	Issuer   string
	Audience string

	JWKSURL      string
	PublicKeyPEM string

	JWKSCacheTTL time.Duration

	SkipPathPrefixes []string
}

func JWTAuthFromEnvironment() JWTAuthConfig {
	ttl := defaults.DefaultJWKSCacheTTL
	if v := os.Getenv("JWT_JWKS_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	return JWTAuthConfig{
		Mode:             strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE"))),
		Issuer:           strings.TrimSpace(os.Getenv("JWT_ISS")),
		Audience:         strings.TrimSpace(os.Getenv("JWT_AUD")),
		JWKSURL:          strings.TrimSpace(os.Getenv("JWT_JWKS_URL")),
		PublicKeyPEM:     strings.TrimSpace(os.Getenv("JWT_PUBLIC_KEY_PEM")),
		JWKSCacheTTL:     ttl,
		SkipPathPrefixes: []string{mcpwire.PathHealthz, mcpwire.PathReadyz},
	}
}

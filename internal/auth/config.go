package auth

import (
	"fmt"
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

func JWTAuthFromEnvironment() (JWTAuthConfig, error) {
	ttl := defaults.DefaultJWKSCacheTTL
	if v := os.Getenv("JWT_JWKS_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	publicKeyPEM, err := publicKeyPEMFromEnvironment()
	if err != nil {
		return JWTAuthConfig{}, err
	}
	return JWTAuthConfig{
		Mode:             strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE"))),
		Issuer:           strings.TrimSpace(os.Getenv("JWT_ISS")),
		Audience:         strings.TrimSpace(os.Getenv("JWT_AUD")),
		JWKSURL:          strings.TrimSpace(os.Getenv("JWT_JWKS_URL")),
		PublicKeyPEM:     publicKeyPEM,
		JWKSCacheTTL:     ttl,
		SkipPathPrefixes: []string{mcpwire.PathHealthz, mcpwire.PathReadyz},
	}, nil
}

func publicKeyPEMFromEnvironment() (string, error) {
	if path := strings.TrimSpace(os.Getenv("JWT_PUBLIC_KEY_FILE")); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read JWT_PUBLIC_KEY_FILE %q: %w", path, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(os.Getenv("JWT_PUBLIC_KEY_PEM")), nil
}

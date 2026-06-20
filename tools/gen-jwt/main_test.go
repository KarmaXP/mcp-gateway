package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func TestResolveAlias(t *testing.T) {
	cases := []struct {
		name  string
		long  string
		alias string
		want  string
	}{
		{name: "alias overrides long", long: "https://dev.local", alias: "https://lab.local", want: "https://lab.local"},
		{name: "long used when alias empty", long: "https://dev.local", alias: "", want: "https://dev.local"},
		{name: "alias whitespace falls back to long", long: "mcp-gateway", alias: "   ", want: "mcp-gateway"},
		{name: "both empty", long: "", alias: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAlias(tc.long, tc.alias); got != tc.want {
				t.Fatalf("resolveAlias(%q, %q) = %q, want %q", tc.long, tc.alias, got, tc.want)
			}
		})
	}
}

func TestDevJWTPairOnDisk(t *testing.T) {
	keyPath := os.Getenv("LAB_JWT_PRIVATE_KEY")
	if keyPath == "" {
		keyPath = "/tmp/mcp-lab-jwt.key"
	}
	pubPath := os.Getenv("LAB_JWT_PUBLIC_KEY")
	if pubPath == "" {
		pubPath = "/tmp/mcp-lab-jwt.pub.pem"
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Skip("lab keys not found; run make lab-jwt-keys")
	}
	pubPEM, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	priv, err := loadRSAPrivateKey(keyPath)
	require.NoError(t, err)

	const iss = "https://lab.local"
	aud := defaults.DefaultTelemetryServiceName
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, devTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    iss,
			Subject:   "lab-verify",
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(devJWTTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(devJWTIssuedSkew)),
		},
		MCPTools: []string{"prom__read_text_file", "k8s__echo", "gh__create_entities"},
	})
	tok.Header["kid"] = "dev-1"
	signed, err := tok.SignedString(priv)
	require.NoError(t, err)

	cfg := auth.JWTAuthConfig{
		Mode:         "jwt",
		Issuer:       iss,
		Audience:     aud,
		PublicKeyPEM: string(pubPEM),
	}
	v, err := auth.NewValidator(cfg)
	require.NoError(t, err)
	require.NoError(t, v.Validate(context.Background(), signed))
}

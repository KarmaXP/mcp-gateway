// Command gen-jwt prints a short-lived RS256 JWT for local smoke tests.
// Usage:
//
//	openssl genrsa -out /tmp/jwt.key 2048
//	openssl rsa -in /tmp/jwt.key -pubout -out /tmp/jwt.pub.pem
//	export JWT_PUBLIC_KEY_PEM="$(cat /tmp/jwt.pub.pem)"
//	go run ./tools/gen-jwt -iss https://lab.local -aud mcp-gateway -key /tmp/jwt.key
//
// The short aliases -iss / -aud / -sub mirror the long flags so the
// evaluation runbooks (scenario-real-backends-jwt.md, integration-checklist.md
// profile B/C) are copy-paste reproducible. When both a long flag and its short
// alias are set, the short alias wins.
package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

const (
	devJWTTokenTTL = time.Hour
	devJWTIssuedSkew = -time.Minute

	exitStatusGeneralError = 1
	exitStatusInvalidUsage = 2
)

const defaultIssuer = "https://dev.local"

type devTokenClaims struct {
	jwt.RegisteredClaims
	MCPTools []string `json:"mcp_tools,omitempty"`
}

func main() {
	issuer := flag.String("issuer", defaultIssuer, "token issuer (iss claim)")
	iss := flag.String("iss", "", "alias for -issuer (wins when both set)")
	audience := flag.String("audience", defaults.DefaultTelemetryServiceName, "token audience (aud claim)")
	aud := flag.String("aud", "", "alias for -audience (wins when both set)")
	subject := flag.String("subject", "", "optional subject (sub claim), recorded in audit logs")
	sub := flag.String("sub", "", "alias for -subject (wins when both set)")
	keyPath := flag.String("key", "", "path to RSA private key PEM")
	kid := flag.String("kid", "dev-1", "")
	mcpTools := flag.String("mcp-tools", "", "optional comma-separated namespaced tool allow-list for claim mcp_tools")
	flag.Parse()
	if *keyPath == "" {
		fmt.Fprintln(os.Stderr, "-key required")
		os.Exit(exitStatusInvalidUsage)
	}
	resolvedIss := resolveAlias(*issuer, *iss)
	resolvedAud := resolveAlias(*audience, *aud)
	resolvedSub := resolveAlias(*subject, *sub)
	tools := parseCSVList(*mcpTools)
	priv, err := loadRSAPrivateKey(*keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitStatusGeneralError)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, devTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    resolvedIss,
			Subject:   resolvedSub,
			Audience:  jwt.ClaimStrings{resolvedAud},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(devJWTTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(devJWTIssuedSkew)),
		},
		MCPTools: tools,
	})
	tok.Header["kid"] = *kid
	s, err := tok.SignedString(priv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitStatusGeneralError)
	}
	fmt.Print(s)
}

func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse key: %w", err)
		}
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not RSA private key")
		}
		return rk, nil
	}
	return k, nil
}

func resolveAlias(long, alias string) string {
	if strings.TrimSpace(alias) != "" {
		return alias
	}
	return long
}

func parseCSVList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

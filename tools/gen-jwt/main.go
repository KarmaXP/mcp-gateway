// Command gen-jwt prints a short-lived RS256 JWT for local smoke tests.
// Usage:
//
//	openssl genrsa -out /tmp/jwt.key 2048
//	openssl rsa -in /tmp/jwt.key -pubout -out /tmp/jwt.pub.pem
//	export JWT_PUBLIC_KEY_PEM="$(cat /tmp/jwt.pub.pem)"
//	go run ./tools/gen-jwt -issuer https://dev -audience mcp -key /tmp/jwt.key
package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
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

func main() {
	iss := flag.String("issuer", "https://dev.local", "")
	aud := flag.String("audience", defaults.DefaultTelemetryServiceName, "")
	keyPath := flag.String("key", "", "path to RSA private key PEM")
	kid := flag.String("kid", "dev-1", "")
	flag.Parse()
	if *keyPath == "" {
		fmt.Fprintln(os.Stderr, "-key required")
		os.Exit(exitStatusInvalidUsage)
	}
	priv, err := loadRSAPrivateKey(*keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitStatusGeneralError)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    *iss,
		Audience:  jwt.ClaimStrings{*aud},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(devJWTTokenTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(devJWTIssuedSkew)),
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

package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// Validator performs stateless JWT checks (signature + exp; optional iss/aud).
type Validator struct {
	cfg    Config
	parser *jwt.Parser

	mu          sync.RWMutex
	pemKey      any
	jwksSet     jwk.Set
	jwksExpires time.Time
}

// NewValidator builds a validator. Mode "none" returns (nil, nil).
func NewValidator(cfg Config) (*Validator, error) {
	if cfg.Mode == "" || cfg.Mode == "none" {
		return nil, nil
	}
	if cfg.Mode != "jwt" {
		return nil, fmt.Errorf("auth: unknown AUTH_MODE %q", cfg.Mode)
	}
	v := &Validator{
		cfg: cfg,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
			jwt.WithIssuedAt(),
			jwt.WithExpirationRequired(),
		),
	}
	if cfg.PublicKeyPEM != "" {
		key, err := parseRSAPublicKey(cfg.PublicKeyPEM)
		if err != nil {
			return nil, err
		}
		v.pemKey = key
	}
	return v, nil
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("auth: jwt: invalid PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: jwt: parse public key: %w", err)
	}
	rsaKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("auth: jwt: expected RSA public key")
	}
	return rsaKey, nil
}

// Validate parses and validates claims without logging the token or raw claims.
func (v *Validator) Validate(ctx context.Context, token string) error {
	var claims jwt.RegisteredClaims
	_, err := v.parser.ParseWithClaims(token, &claims, func(t *jwt.Token) (interface{}, error) {
		if v.pemKey != nil {
			return v.pemKey, nil
		}
		return v.keyFromJWKS(ctx, t)
	})
	if err != nil {
		return fmt.Errorf("auth: jwt: %w", err)
	}
	if v.cfg.Issuer != "" && claims.Issuer != v.cfg.Issuer {
		return fmt.Errorf("auth: jwt: invalid iss")
	}
	if v.cfg.Audience != "" {
		ok := false
		for _, a := range claims.Audience {
			if a == v.cfg.Audience {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("auth: jwt: invalid aud")
		}
	}
	return nil
}

func (v *Validator) keyFromJWKS(ctx context.Context, t *jwt.Token) (interface{}, error) {
	if v.cfg.JWKSURL == "" {
		return nil, fmt.Errorf("auth: jwt: JWKS URL or JWT_PUBLIC_KEY_PEM required")
	}
	kid, _ := t.Header["kid"].(string)

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.jwksSet == nil || time.Now().After(v.jwksExpires) {
		set, err := jwk.Fetch(ctx, v.cfg.JWKSURL)
		if err != nil {
			return nil, fmt.Errorf("auth: jwks fetch: %w", err)
		}
		v.jwksSet = set
		v.jwksExpires = time.Now().Add(v.cfg.JWKSCacheTTL)
	}
	if kid == "" {
		return nil, fmt.Errorf("auth: jwt: missing kid (required for JWKS)")
	}
	key, ok := v.jwksSet.LookupKeyID(kid)
	if !ok {
		return nil, fmt.Errorf("auth: jwks: unknown kid %q", kid)
	}
	return publicKeyFromJWK(key)
}

func publicKeyFromJWK(key jwk.Key) (interface{}, error) {
	var raw interface{}
	if err := key.Raw(&raw); err != nil {
		return nil, fmt.Errorf("auth: jwk raw: %w", err)
	}
	return raw, nil
}

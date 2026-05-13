package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

type Validator struct {
	cfg    JWTAuthConfig
	parser *jwt.Parser

	mu          sync.RWMutex
	pemKey      any
	jwksSet     jwk.Set
	jwksExpires time.Time
}

func NewValidator(cfg JWTAuthConfig) (*Validator, error) {
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

func (v *Validator) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(t *jwt.Token) (interface{}, error) {
		if v.pemKey != nil {
			return v.pemKey, nil
		}
		return v.keyFromJWKS(ctx, t)
	}
}

func (v *Validator) checkIssAud(c *jwt.RegisteredClaims) error {
	if v.cfg.Issuer != "" && c.Issuer != v.cfg.Issuer {
		return fmt.Errorf("auth: jwt: invalid iss")
	}
	if v.cfg.Audience != "" {
		ok := false
		for _, a := range c.Audience {
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

func (v *Validator) Validate(ctx context.Context, token string) error {
	var claims jwt.RegisteredClaims
	_, err := v.parser.ParseWithClaims(token, &claims, v.keyFunc(ctx))
	if err != nil {
		return fmt.Errorf("auth: jwt: %w", err)
	}
	return v.checkIssAud(&claims)
}

func (v *Validator) ParseTokenClaims(ctx context.Context, token string) (*TokenClaims, error) {
	var claims TokenClaims
	_, err := v.parser.ParseWithClaims(token, &claims, v.keyFunc(ctx))
	if err != nil {
		return nil, fmt.Errorf("auth: jwt: %w", err)
	}
	if err := v.checkIssAud(&claims.RegisteredClaims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func (v *Validator) ValidateWithAllowedTools(ctx context.Context, token string) ([]string, error) {
	c, err := v.ParseTokenClaims(ctx, token)
	if err != nil {
		return nil, err
	}
	return c.NormalizedMcpTools(), nil
}

func normalizeMcpToolNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (v *Validator) keyFromJWKS(ctx context.Context, t *jwt.Token) (interface{}, error) {
	if v.cfg.JWKSURL == "" {
		return nil, fmt.Errorf("auth: jwt: JWKS URL or JWT_PUBLIC_KEY_PEM required")
	}
	kid, _ := t.Header["kid"].(string)

	v.mu.Lock()
	defer v.mu.Unlock()

	ttlExpired := v.jwksSet == nil || time.Now().After(v.jwksExpires)
	if ttlExpired {
		if err := v.fetchJWKSLocked(ctx); err != nil {
			return nil, err
		}
	}
	if kid == "" {
		telemetry.RecordJWKSLookup(ctx, defaults.MetricJWKSResultErrorMissingKid)
		return nil, fmt.Errorf("auth: jwt: missing kid (required for JWKS)")
	}

	key, ok := v.jwksSet.LookupKeyID(kid)
	unknownKidRefresh := false
	if !ok && !ttlExpired {
		if err := v.fetchJWKSLocked(ctx); err != nil {
			return nil, err
		}
		unknownKidRefresh = true
		key, ok = v.jwksSet.LookupKeyID(kid)
	}
	if !ok {
		telemetry.RecordJWKSLookup(ctx, defaults.MetricJWKSResultErrorUnknownKid)
		return nil, fmt.Errorf("auth: jwks: unknown kid %q", kid)
	}

	lookupResult := defaults.MetricJWKSResultHit
	switch {
	case ttlExpired:
		lookupResult = defaults.MetricJWKSResultRefresh
	case unknownKidRefresh:
		lookupResult = defaults.MetricJWKSResultRefreshUnknownKid
	}
	telemetry.RecordJWKSLookup(ctx, lookupResult)
	return publicKeyFromJWK(key)
}

func (v *Validator) fetchJWKSLocked(ctx context.Context) error {
	set, err := jwk.Fetch(ctx, v.cfg.JWKSURL)
	if err != nil {
		telemetry.RecordJWKSLookup(ctx, defaults.MetricJWKSResultErrorFetch)
		return fmt.Errorf("auth: jwks fetch: %w", err)
	}
	v.jwksSet = set
	v.jwksExpires = time.Now().Add(v.cfg.JWKSCacheTTL)
	return nil
}

func publicKeyFromJWK(key jwk.Key) (interface{}, error) {
	var raw interface{}
	if err := key.Raw(&raw); err != nil {
		return nil, fmt.Errorf("auth: jwk raw: %w", err)
	}
	return raw, nil
}

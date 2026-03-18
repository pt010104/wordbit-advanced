package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"wordbit-advanced-app/backend/internal/config"
)

type Subject struct {
	Subject string
	Email   string
}

type Verifier struct {
	devBypass  bool
	devSubject Subject
	jwksURL    string
	issuer     string
	audience   string
	httpClient *http.Client
	logger     *slog.Logger

	mu        sync.RWMutex
	keys      map[string]any
	expiresAt time.Time
}

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KID string `json:"kid"`
	KTY string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func NewVerifier(cfg config.AuthConfig, logger *slog.Logger) *Verifier {
	return &Verifier{
		devBypass: cfg.DevBypass,
		devSubject: Subject{
			Subject: cfg.DevSubject,
			Email:   cfg.DevEmail,
		},
		jwksURL:  cfg.JWKSURL,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
		keys:   map[string]any{},
	}
}

func (v *Verifier) Verify(ctx context.Context, tokenString string) (Subject, error) {
	if v.devBypass {
		return v.devSubject, nil
	}
	if tokenString == "" {
		return Subject{}, errors.New("missing bearer token")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid")
		}
		return v.keyFor(ctx, kid)
	}, jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience))
	if err != nil {
		return Subject{}, err
	}
	if !token.Valid {
		return Subject{}, errors.New("invalid token")
	}
	return Subject{
		Subject: claims.Subject,
		Email:   claims.Email,
	}, nil
}

func (v *Verifier) keyFor(ctx context.Context, kid string) (any, error) {
	v.mu.RLock()
	if key, ok := v.keys[kid]; ok && time.Now().Before(v.expiresAt) {
		v.mu.RUnlock()
		return key, nil
	}
	v.mu.RUnlock()

	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	key, ok := v.keys[kid]
	if !ok {
		return nil, fmt.Errorf("signing key %q not found", kid)
	}
	return key, nil
}

func (v *Verifier) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jwks request failed with status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	keys := map[string]any{}
	for _, key := range doc.Keys {
		parsed, err := parseJWK(key)
		if err != nil {
			v.logger.Warn("skip unsupported jwk", "kid", key.KID, "error", err)
			continue
		}
		keys[key.KID] = parsed
	}
	if len(keys) == 0 {
		return errors.New("jwks did not contain supported keys")
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys = keys
	v.expiresAt = time.Now().Add(1 * time.Hour)
	return nil
}

func parseJWK(key jwk) (any, error) {
	switch strings.ToUpper(key.KTY) {
	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			return nil, err
		}
		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		return &rsa.PublicKey{N: n, E: e}, nil
	case "EC":
		curve := elliptic.P256()
		switch key.Crv {
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
		if err != nil {
			return nil, err
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
		if err != nil {
			return nil, err
		}
		pub := &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, errors.New("invalid ec key")
		}
		return pub, nil
	default:
		return nil, fmt.Errorf("unsupported key type %s", key.KTY)
	}
}

func ParseBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

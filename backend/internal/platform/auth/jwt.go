package auth

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload for esaProperti tokens.
type Claims struct {
	TenantID uint64 `json:"tid"`
	UserID   uint64 `json:"uid"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Generate signs a new HS256 JWT with the given claims.
func Generate(secret string, tenantID, userID uint64, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		TenantID: tenantID,
		UserID:   userID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// Parse validates and parses a JWT string. Returns ErrInvalidToken if malformed or expired.
func Parse(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	if claims.TenantID == 0 || claims.UserID == 0 {
		return nil, errors.New("token missing tenant or user id")
	}
	return claims, nil
}

// ── Context helpers ───────────────────────────────────────────────────────────

type claimsKey struct{}

// WithClaims stores parsed claims in ctx.
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFrom retrieves claims from ctx. Returns nil, false if absent.
func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(*Claims)
	return c, ok && c != nil
}

// TenantIDFrom extracts the tenant ID from ctx claims. Use in handlers.
func TenantIDFrom(ctx context.Context) (uint64, bool) {
	c, ok := ClaimsFrom(ctx)
	if !ok {
		return 0, false
	}
	return c.TenantID, true
}

// UserIDFrom extracts the user ID from ctx claims.
func UserIDFrom(ctx context.Context) (uint64, bool) {
	c, ok := ClaimsFrom(ctx)
	if !ok {
		return 0, false
	}
	return c.UserID, true
}

// RoleFrom extracts the role string from ctx claims.
func RoleFrom(ctx context.Context) (string, bool) {
	c, ok := ClaimsFrom(ctx)
	if !ok {
		return "", false
	}
	return c.Role, true
}

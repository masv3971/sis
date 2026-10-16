// Package token issues and verifies the service's JWT access tokens (HS256).
package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Kind distinguishes a human login token from a machine client token.
type Kind string

const (
	KindUser   Kind = "user"
	KindClient Kind = "client"
)

// Claims is the token payload.
type Claims struct {
	Kind   Kind     `json:"typ"`
	Roles  []string `json:"roles,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
	jwt.RegisteredClaims
}

// Issuer signs and verifies tokens with a shared secret.
type Issuer struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewIssuer returns an Issuer. ttl is the token lifetime.
func NewIssuer(secret, issuer string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), issuer: issuer, ttl: ttl}
}

// Issue signs a token for subject with the given kind, roles and scopes.
func (i *Issuer) Issue(subject string, kind Kind, roles, scopes []string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.ttl)
	claims := Claims{
		Kind:   kind,
		Roles:  roles,
		Scopes: scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    i.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	return signed, exp, err
}

// Parse verifies a token and returns its claims.
func (i *Issuer) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return i.secret, nil
	}, jwt.WithIssuer(i.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	return claims, nil
}

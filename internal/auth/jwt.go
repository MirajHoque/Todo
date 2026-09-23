// Package auth handles JWT token creation and validation.
//
// WHY A SEPARATE PACKAGE?
// JWT logic is neither HTTP (handlers) nor database (db).
// It is a pure service — takes a user ID, returns a token string,
// or takes a token string, returns a user ID.
// Keeping it separate means both handlers AND middleware can use it
// without circular imports.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims defines what we store inside the JWT payload.
// jwt.RegisteredClaims adds standard fields like ExpiresAt, IssuedAt.
// We add our own UserID field on top.
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// Service handles JWT creation and validation.
type Service struct {
	secret []byte
	expiry time.Duration
}

// NewService creates a JWT service.
// secret is your JWT_SECRET from .env.
// expiry is how long tokens are valid (e.g. 24h).
func NewService(secret string, expiry time.Duration) *Service {
	return &Service{
		secret: []byte(secret),
		expiry: expiry,
	}
}

// CreateToken generates a signed JWT token for the given user ID.
// Called by the login and register handlers after password verification.
//
// The token payload contains:
//   - user_id: who this token belongs to
//   - exp:     when it expires (now + expiry duration)
//   - iat:     when it was issued (now)
func (s *Service) CreateToken(userID string) (string, error) {
	now := time.Now()

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "todo-app",
		},
	}

	// jwt.NewWithClaims creates the token with HS256 signing method.
	// SignedString(secret) produces the final token string.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}

	return signed, nil
}

// ValidateToken parses and validates a JWT token string.
// Returns the claims (including user_id) if valid.
// Returns an error if:
//   - the token is malformed
//   - the signature doesn't match (tampered)
//   - the token has expired
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		// This function is called by the JWT library to get the signing key.
		// We verify the algorithm matches what we use (HS256) to prevent
		// algorithm confusion attacks where an attacker sends "alg: none".
		func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("auth: unexpected signing method: %v", token.Header["alg"])
			}
			return s.secret, nil
		},
	)

	if err != nil {
		// Convert jwt library errors to our own descriptive errors.
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

// ---- Sentinel errors --------------------------------------------------------

// ErrTokenInvalid is returned when a token cannot be parsed or verified.
var ErrTokenInvalid = errors.New("token is invalid")

// ErrTokenExpired is returned when a token's expiry time has passed.
var ErrTokenExpired = errors.New("token has expired")

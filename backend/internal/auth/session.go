package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type SessionManager struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func NewSessionManager(key []byte, ttl time.Duration, now func() time.Time) *SessionManager {
	if now == nil {
		now = time.Now
	}
	return &SessionManager{key: key, ttl: ttl, now: now}
}

func (m *SessionManager) Issue(userID uuid.UUID) (string, error) {
	now := m.now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.key)
}

func (m *SessionManager) Validate(token string) (uuid.UUID, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return m.key, nil
		},
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		return uuid.Nil, err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid {
		return uuid.Nil, fmt.Errorf("invalid token")
	}
	return uuid.Parse(claims.Subject)
}

package auth

import (
	"context"
	"errors"
)

var ErrInvalidToken = errors.New("invalid id token")

type Identity struct {
	Provider   string
	Subject    string
	Email      string
	Name       string
	PictureURL string
}

type IDTokenVerifier interface {
	Verify(ctx context.Context, provider, rawToken string) (Identity, error)
}

// FakeVerifier is a test double mapping raw tokens to identities.
type FakeVerifier struct {
	Identities map[string]Identity
	Err        error
}

func (f *FakeVerifier) Verify(_ context.Context, provider, rawToken string) (Identity, error) {
	if f.Err != nil {
		return Identity{}, f.Err
	}
	id, ok := f.Identities[rawToken]
	if !ok {
		return Identity{}, ErrInvalidToken
	}
	return id, nil
}

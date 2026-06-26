package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

type oidcClaims struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type OIDCVerifier struct {
	verifiers map[string]*oidc.IDTokenVerifier
}

// NewOIDCVerifier wires Google and Apple issuers with their audiences.
func NewOIDCVerifier(ctx context.Context, googleAud, appleAud string) (*OIDCVerifier, error) {
	v := &OIDCVerifier{verifiers: map[string]*oidc.IDTokenVerifier{}}
	google, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("google provider: %w", err)
	}
	v.verifiers["google"] = google.Verifier(&oidc.Config{ClientID: googleAud})
	apple, err := oidc.NewProvider(ctx, "https://appleid.apple.com")
	if err != nil {
		return nil, fmt.Errorf("apple provider: %w", err)
	}
	v.verifiers["apple"] = apple.Verifier(&oidc.Config{ClientID: appleAud})
	return v, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, provider, rawToken string) (Identity, error) {
	ver, ok := v.verifiers[provider]
	if !ok {
		return Identity{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidToken, provider)
	}
	tok, err := ver.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	var c oidcClaims
	if err := tok.Claims(&c); err != nil {
		return Identity{}, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err)
	}
	return Identity{
		Provider: provider, Subject: tok.Subject,
		Email: c.Email, Name: c.Name, PictureURL: c.Picture,
	}, nil
}

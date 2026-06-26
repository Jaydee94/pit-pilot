package auth

import (
	"context"
	"errors"
	"testing"
)

func TestFakeVerifierReturnsMappedIdentity(t *testing.T) {
	var v IDTokenVerifier = &FakeVerifier{
		Identities: map[string]Identity{
			"tok-abc": {Provider: "google", Subject: "s1", Email: "a@x.io", Name: "Ann"},
		},
	}
	id, err := v.Verify(context.Background(), "google", "tok-abc")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "s1" || id.Provider != "google" {
		t.Fatalf("unexpected identity: %+v", id)
	}
}

func TestFakeVerifierUnknownTokenErrors(t *testing.T) {
	v := &FakeVerifier{Identities: map[string]Identity{}}
	if _, err := v.Verify(context.Background(), "google", "nope"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

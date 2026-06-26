package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/auth"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestLoginUpsertsUser(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	v := &auth.FakeVerifier{Identities: map[string]auth.Identity{
		"tok": {Provider: "google", Subject: "sub-9", Email: "z@x.io", Name: "Zed"},
	}}
	svc := service.NewUserService(q, v)

	u, err := svc.LoginWithIDToken(context.Background(), "google", "tok")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if u.DisplayName != "Zed" || u.Email != "z@x.io" {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestLoginRejectsInvalidToken(t *testing.T) {
	pool := testutil.NewPostgres(t)
	svc := service.NewUserService(gen.New(pool), &auth.FakeVerifier{Identities: map[string]auth.Identity{}})
	_, err := svc.LoginWithIDToken(context.Background(), "google", "bad")
	appErr, ok := apperr.As(err)
	if !ok || appErr.HTTPStatus != 401 {
		t.Fatalf("expected 401 apperr, got %v", err)
	}
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("expected wrapped ErrInvalidToken, got %v", err)
	}
}

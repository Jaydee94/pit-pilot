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

func TestRegisterAndLoginWithPassword(t *testing.T) {
	pool := testutil.NewPostgres(t)
	svc := service.NewUserService(gen.New(pool), nil)
	ctx := context.Background()

	u, err := svc.Register(ctx, "user@example.com", "hunter2hunter", "User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.Provider != "password" || u.DisplayName != "User" {
		t.Fatalf("unexpected user: %+v", u)
	}

	// duplicate → Conflict
	if _, err := svc.Register(ctx, "user@example.com", "another8x", "Dup"); err == nil {
		t.Fatal("expected conflict on duplicate email")
	}

	// short password → BadRequest (no row created)
	if _, err := svc.Register(ctx, "short@example.com", "x", "Short"); err == nil {
		t.Fatal("expected error for short password")
	}

	got, err := svc.LoginWithPassword(ctx, "user@example.com", "hunter2hunter")
	if err != nil || got.ID != u.ID {
		t.Fatalf("login should succeed: %+v err=%v", got, err)
	}
	if _, err := svc.LoginWithPassword(ctx, "user@example.com", "wrongpass1"); err == nil {
		t.Fatal("login with wrong password should fail")
	}
	if _, err := svc.LoginWithPassword(ctx, "nobody@example.com", "whatever1"); err == nil {
		t.Fatal("login with unknown email should fail")
	}
}

func TestDevLoginIsIdempotent(t *testing.T) {
	pool := testutil.NewPostgres(t)
	svc := service.NewUserService(gen.New(pool), nil)
	ctx := context.Background()
	a, err := svc.DevLogin(ctx, "Alice")
	if err != nil || a.Provider != "dummy" {
		t.Fatalf("dev login: %+v err=%v", a, err)
	}
	b, err := svc.DevLogin(ctx, "Alice")
	if err != nil || b.ID != a.ID {
		t.Fatalf("dev login not idempotent: %v vs %v", a.ID, b.ID)
	}
}

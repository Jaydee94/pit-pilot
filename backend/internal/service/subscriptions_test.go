// backend/internal/service/subscriptions_test.go
package service_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestSaveAndDeleteSubscription(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	svc := service.NewSubscriptionService(q)
	ctx := context.Background()
	u := seedUser(t, q, "u")

	sub, err := svc.Save(ctx, u.ID, "https://push/x", "k", "a")
	if err != nil || sub.UserID != u.ID {
		t.Fatalf("save: %v err=%v", sub, err)
	}
	if err := svc.Delete(ctx, u.ID, "https://push/x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestSaveRejectsEmptyEndpoint(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	svc := service.NewSubscriptionService(q)
	u := seedUser(t, q, "u")
	_, err := svc.Save(context.Background(), u.ID, "", "k", "a")
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
}

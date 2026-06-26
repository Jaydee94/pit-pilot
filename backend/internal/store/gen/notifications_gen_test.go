package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestInsertAndClaimNotification(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "n", DisplayName: "N"})

	if err := q.InsertNotification(ctx, gen.InsertNotificationParams{
		UserID: u.ID, Type: "concert_new", Title: "T", Body: "B"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := q.ClaimPendingNotifications(ctx, 10)
	if err != nil || len(rows) != 1 || rows[0].Status != "pending" {
		t.Fatalf("claim: %v err=%v", rows, err)
	}
	if err := q.MarkNotificationSent(ctx, rows[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
}

func TestUpsertPushSubscriptionIdempotentOnEndpoint(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "n", DisplayName: "N"})
	p := gen.UpsertPushSubscriptionParams{UserID: u.ID, Endpoint: "https://push/x", P256dh: "k", Auth: "a"}
	s1, _ := q.UpsertPushSubscription(ctx, p)
	p.Auth = "a2"
	s2, err := q.UpsertPushSubscription(ctx, p)
	if err != nil || s1.ID != s2.ID || s2.Auth != "a2" {
		t.Fatalf("upsert not idempotent: %v %v err=%v", s1, s2, err)
	}
}

// backend/internal/worker/worker_test.go
package worker_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/push"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
	"github.com/jaydee94/pit-pilot/backend/internal/worker"
)

func seedNotif(t *testing.T, q *gen.Queries) (gen.User, gen.PushSubscription) {
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "w", DisplayName: "W"})
	sub, _ := q.UpsertPushSubscription(ctx, gen.UpsertPushSubscriptionParams{UserID: u.ID, Endpoint: "https://push/x", P256dh: "k", Auth: "a"})
	_ = q.InsertNotification(ctx, gen.InsertNotificationParams{UserID: u.ID, Type: "concert_new", Title: "T", Body: "B"})
	return u, sub
}

func TestWorkerSendsPending(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	u, _ := seedNotif(t, q)
	fp := &push.FakePusher{}
	if err := worker.New(pool, q, fp).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(fp.Sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(fp.Sent))
	}
	rem, _ := q.ClaimPendingNotifications(context.Background(), 10)
	if len(rem) != 0 {
		t.Fatalf("expected no pending left, got %d", len(rem))
	}
	_ = u
}

func TestWorkerDeletesDeadSubscription(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	u, _ := seedNotif(t, q)
	fp := &push.FakePusher{Dead: true}
	if err := worker.New(pool, q, fp).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	subs, _ := q.ListPushSubscriptionsForUser(context.Background(), u.ID)
	if len(subs) != 0 {
		t.Fatalf("expected dead subscription deleted, got %d", len(subs))
	}
}

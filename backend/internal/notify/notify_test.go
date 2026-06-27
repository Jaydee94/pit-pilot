package notify_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestOutboxEnqueuerInserts(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "u", DisplayName: "U"})

	var e notify.Enqueuer = notify.OutboxEnqueuer{}
	url := "/concerts/1"
	if err := e.Enqueue(ctx, q, []notify.Notification{
		{UserID: u.ID, Type: "concert_new", Title: "T", Body: "B", URL: url},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	rows, _ := q.ClaimPendingNotifications(ctx, 10)
	if len(rows) != 1 || rows[0].Url == nil || *rows[0].Url != "/concerts/1" {
		t.Fatalf("expected 1 row with url, got %v", rows)
	}
}

func TestFakeEnqueuerRecords(t *testing.T) {
	f := &notify.FakeEnqueuer{}
	_ = f.Enqueue(context.Background(), nil, []notify.Notification{{Type: "payment"}})
	if len(f.Rows) != 1 || f.Rows[0].Type != "payment" {
		t.Fatalf("fake did not record: %v", f.Rows)
	}
}

package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestUpsertUserIsIdempotentOnProviderSub(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	ctx := context.Background()

	u1, err := q.UpsertUser(ctx, gen.UpsertUserParams{
		Provider: "google", ProviderSub: "sub-1", Email: "a@x.io", DisplayName: "Ann",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	u2, err := q.UpsertUser(ctx, gen.UpsertUserParams{
		Provider: "google", ProviderSub: "sub-1", Email: "a@x.io", DisplayName: "Annie",
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if u1.ID != u2.ID {
		t.Fatalf("expected same user id, got %s vs %s", u1.ID, u2.ID)
	}
	if u2.DisplayName != "Annie" {
		t.Fatalf("expected display name updated, got %q", u2.DisplayName)
	}
}

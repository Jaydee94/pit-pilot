package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestUpsertRSVPChangesStatus(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	u, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "u", DisplayName: "U"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "Z9", CreatedBy: u.ID})
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{GroupID: g.ID, Artist: "A", EventAt: when, RsvpDeadline: when.Add(-time.Hour), CreatedBy: u.ID})

	if _, err := q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: u.ID, Status: "yes"}); err != nil {
		t.Fatalf("first rsvp: %v", err)
	}
	r2, err := q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: u.ID, Status: "no"})
	if err != nil || r2.Status != "no" {
		t.Fatalf("rsvp change failed: %v err=%v", r2, err)
	}
}

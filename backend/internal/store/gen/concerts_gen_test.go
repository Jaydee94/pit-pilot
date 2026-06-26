package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestCreateAndListConcert(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "X1", CreatedBy: owner.ID})

	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, err := q.CreateConcert(ctx, gen.CreateConcertParams{
		GroupID: g.ID, Artist: "Tool", EventAt: when,
		RsvpDeadline: when.Add(-14 * 24 * time.Hour), CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create concert: %v", err)
	}
	list, err := q.ListConcertsForGroup(ctx, g.ID)
	if err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list concerts: %v err=%v", list, err)
	}
}

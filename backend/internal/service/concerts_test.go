// backend/internal/service/concerts_test.go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func newConcertSetup(t *testing.T) (*service.ConcertService, *service.GroupService, *gen.Queries) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "INV" + string(rune('A'+n)) })
	return service.NewConcertService(q, gs), gs, q
}

func TestCreateConcertMemberOnly(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	in := service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-72 * time.Hour)}

	if _, err := cs.Create(ctx, stranger.ID, g.ID, in); err == nil {
		t.Fatal("stranger must not create concert")
	}
	c, err := cs.Create(ctx, owner.ID, g.ID, in)
	if err != nil || c.Artist != "Tool" {
		t.Fatalf("owner create failed: %v err=%v", c, err)
	}
}

func TestCreateConcertRejectsDeadlineAfterEvent(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	in := service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(time.Hour)}
	_, err := cs.Create(ctx, owner.ID, g.ID, in)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
}

func TestGetConcertGatesOnMembership(t *testing.T) {
	cs, gs, q := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	if _, err := cs.Get(ctx, stranger.ID, c.ID); err == nil {
		t.Fatal("stranger must not read concert")
	}
}

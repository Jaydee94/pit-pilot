// backend/internal/service/rsvps_test.go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestRSVPSetAndList(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "R" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	ctx := context.Background()

	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})

	if _, err := rs.Set(ctx, owner.ID, c.ID, "yes"); err != nil {
		t.Fatalf("set rsvp: %v", err)
	}
	rows, err := rs.List(ctx, owner.ID, c.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "yes" {
		t.Fatalf("list rsvps: %v err=%v", rows, err)
	}
}

func TestRSVPRejectsBadStatus(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "S" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})

	_, err := rs.Set(ctx, owner.ID, c.ID, "maybe")
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for bad status, got %v", err)
	}
}

func TestSetYesEnqueuesRsvpChanged(t *testing.T) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "RX" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	rs := service.NewRSVPService(q, cs)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	friend := seedUser(t, q, "friend")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	_, _ = rs.Set(ctx, owner.ID, c.ID, "yes") // owner already in

	fake := &notify.FakeEnqueuer{}
	rs.SetEnqueuer(fake, pool)
	if _, err := rs.Set(ctx, friend.ID, c.ID, "yes"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(fake.Rows) != 1 || fake.Rows[0].UserID != owner.ID || fake.Rows[0].Type != "rsvp_changed" {
		t.Fatalf("expected 1 rsvp_changed to owner, got %+v", fake.Rows)
	}
}

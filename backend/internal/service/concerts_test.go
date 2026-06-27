// backend/internal/service/concerts_test.go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/notify"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func newConcertSetup(t *testing.T) (*service.ConcertService, *service.GroupService, *gen.Queries, *pgxpool.Pool) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "INV" + string(rune('A'+n)) })
	return service.NewConcertService(q, gs), gs, q, pool
}

func TestCreateConcertMemberOnly(t *testing.T) {
	cs, gs, q, _ := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	in := service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-72 * time.Hour)}

	_, err := cs.Create(ctx, stranger.ID, g.ID, in)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 403 {
		t.Fatalf("expected 403 for non-member, got %v", err)
	}
	c, err := cs.Create(ctx, owner.ID, g.ID, in)
	if err != nil || c.Artist != "Tool" {
		t.Fatalf("owner create failed: %v err=%v", c, err)
	}
}

func TestCreateConcertRejectsDeadlineAfterEvent(t *testing.T) {
	cs, gs, q, _ := newConcertSetup(t)
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
	cs, gs, q, _ := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	_, err := cs.Get(ctx, stranger.ID, c.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 403 {
		t.Fatalf("expected 403 for non-member, got %v", err)
	}
}

func TestListForGroupMemberGated(t *testing.T) {
	cs, gs, q, _ := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	list, err := cs.ListForGroup(ctx, owner.ID, g.ID)
	if err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("member list: %v err=%v", list, err)
	}
	_, err = cs.ListForGroup(ctx, stranger.ID, g.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 403 {
		t.Fatalf("expected 403 for non-member, got %v", err)
	}
}

func TestCreateConcertValidation(t *testing.T) {
	cs, gs, q, _ := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)

	// empty artist
	_, err := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for empty artist, got %v", err)
	}
	// zero dates
	_, err = cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool"})
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for zero dates, got %v", err)
	}
}

func TestGetConcertNotFound(t *testing.T) {
	cs, _, q, _ := newConcertSetup(t)
	owner := seedUser(t, q, "owner")
	_, err := cs.Get(context.Background(), owner.ID, uuid.New())
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 404 {
		t.Fatalf("expected 404 for missing concert, got %v", err)
	}
}

func TestCreateConcertEnqueuesConcertNew(t *testing.T) {
	cs, gs, q, pool := newConcertSetup(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	friend := seedUser(t, q, "friend")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)

	fake := &notify.FakeEnqueuer{}
	cs.SetEnqueuer(fake, pool)

	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	if _, err := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(fake.Rows) != 1 || fake.Rows[0].UserID != friend.ID || fake.Rows[0].Type != "concert_new" {
		t.Fatalf("expected 1 concert_new to friend, got %+v", fake.Rows)
	}
}

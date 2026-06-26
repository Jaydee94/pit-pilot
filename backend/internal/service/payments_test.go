// backend/internal/service/payments_test.go
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

// paySetup builds the payment service stack on a fresh DB plus a concert in a
// group, and returns the services, queries, the concert, and the owner.
func paySetup(t *testing.T) (*service.PaymentService, *service.GroupService, *gen.Queries, gen.Concert, gen.User) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	n := 0
	gs := service.NewGroupService(pool, q, func() string { n++; return "PAY" + string(rune('A'+n)) })
	cs := service.NewConcertService(q, gs)
	ps := service.NewPaymentService(pool, q, cs)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := gs.Create(ctx, owner.ID, "Crew")
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := cs.Create(ctx, owner.ID, g.ID, service.ConcertInput{Artist: "Tool", EventAt: when, RSVPDeadline: when.Add(-time.Hour)})
	return ps, gs, q, c, owner
}

func TestActivateSeedsFromYesRsvps(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	// a second member who RSVPs yes
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})

	col, err := ps.Activate(ctx, owner.ID, c.ID, 4500, nil)
	if err != nil || col.ResponsibleUserID != owner.ID {
		t.Fatalf("activate: %v err=%v", col, err)
	}
	view, err := ps.Get(ctx, owner.ID, c.ID)
	if err != nil || !view.IsResponsible || len(view.Items) != 2 {
		t.Fatalf("responsible view: %+v err=%v", view, err)
	}
	if view.Summary == nil || view.Summary.OutstandingCents != 9000 {
		t.Fatalf("summary wrong: %+v", view.Summary)
	}
}

func TestActivateTwiceConflicts(t *testing.T) {
	ps, _, _, c, owner := paySetup(t)
	ctx := context.Background()
	if _, err := ps.Activate(ctx, owner.ID, c.ID, 1000, nil); err != nil {
		t.Fatalf("first activate: %v", err)
	}
	_, err := ps.Activate(ctx, owner.ID, c.ID, 1000, nil)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 409 {
		t.Fatalf("expected 409, got %v", err)
	}
}

func TestGetMemberSeesOnlyOwnItem(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	view, err := ps.Get(ctx, friend.ID, c.ID)
	if err != nil || view.IsResponsible || len(view.Items) != 1 || view.Items[0].UserID != friend.ID {
		t.Fatalf("member view: %+v err=%v", view, err)
	}
	if view.Summary != nil {
		t.Fatalf("member must not see summary")
	}
}

func TestGetNotActiveIs404(t *testing.T) {
	ps, _, _, c, owner := paySetup(t)
	_, err := ps.Get(context.Background(), owner.ID, c.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

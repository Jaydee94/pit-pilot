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

func TestActivateRejectsNegativeAmount(t *testing.T) {
	ps, _, _, c, owner := paySetup(t)
	_, err := ps.Activate(context.Background(), owner.ID, c.ID, -100, nil)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for negative amount, got %v", err)
	}
}

func TestSetAmountRejectsNegative(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)
	row, _ := ps.AddItem(ctx, owner.ID, c.ID, friend.ID, nil)
	_, err := ps.SetAmount(ctx, owner.ID, c.ID, row.ID, -1)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for negative amount, got %v", err)
	}
}

func TestItemManagementResponsibleOnly(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil) // no yes-rsvps yet → empty list

	// responsible adds an item for friend at default
	row, err := ps.AddItem(ctx, owner.ID, c.ID, friend.ID, nil)
	if err != nil || row.AmountCents != 4500 || row.UserID != friend.ID {
		t.Fatalf("add item: %+v err=%v", row, err)
	}
	// adding the same user again conflicts (409)
	if _, err := ps.AddItem(ctx, owner.ID, c.ID, friend.ID, nil); err == nil {
		t.Fatal("expected conflict adding duplicate item")
	} else if e, ok := apperr.As(err); !ok || e.HTTPStatus != 409 {
		t.Fatalf("expected 409 conflict, got %v", err)
	}
	// a non-responsible member cannot add
	if _, err := ps.AddItem(ctx, friend.ID, c.ID, owner.ID, nil); err == nil {
		t.Fatal("non-responsible must not add items")
	}
	// set amount + remove
	upd, err := ps.SetAmount(ctx, owner.ID, c.ID, row.ID, 3000)
	if err != nil || upd.AmountCents != 3000 {
		t.Fatalf("set amount: %+v err=%v", upd, err)
	}
	if err := ps.RemoveItem(ctx, owner.ID, c.ID, row.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestSetPaymentLink(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	link := "https://paypal.me/owner/45"
	if _, err := ps.SetPaymentLink(ctx, owner.ID, c.ID, &link); err != nil {
		t.Fatalf("set link: %v", err)
	}
	// the owing member sees the link on their view
	view, err := ps.Get(ctx, friend.ID, c.ID)
	if err != nil || view.PaymentLink == nil || *view.PaymentLink != link {
		t.Fatalf("member should see link: %+v err=%v", view.PaymentLink, err)
	}
	// a non-responsible member cannot set it
	if _, err := ps.SetPaymentLink(ctx, friend.ID, c.ID, &link); err == nil {
		t.Fatal("non-responsible must not set the link")
	}
	// clearing with nil removes the link
	if _, err := ps.SetPaymentLink(ctx, owner.ID, c.ID, nil); err != nil {
		t.Fatalf("clear link: %v", err)
	}
	cleared, _ := ps.Get(ctx, friend.ID, c.ID)
	if cleared.PaymentLink != nil {
		t.Fatalf("expected link cleared, got %v", cleared.PaymentLink)
	}
}

func TestDeactivateResponsibleOnly(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	if err := ps.Deactivate(ctx, friend.ID, c.ID); err == nil {
		t.Fatal("non-responsible must not deactivate")
	}
	if err := ps.Deactivate(ctx, owner.ID, c.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := ps.Get(ctx, owner.ID, c.ID); err == nil {
		t.Fatal("expected 404 after deactivate")
	}
}

func paySetupWithItem(t *testing.T) (*service.PaymentService, *gen.Queries, gen.Concert, gen.User, gen.User, gen.ListPaymentItemsRow) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	friend := seedUser(t, q, "friend")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, friend.ID, g.InviteCode)
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: friend.ID, Status: "yes"})
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)
	view, _ := ps.Get(ctx, owner.ID, c.ID)
	return ps, q, c, owner, friend, view.Items[0] // friend's item
}

func TestReportConfirmRoundTrip(t *testing.T) {
	ps, _, c, owner, friend, item := paySetupWithItem(t)
	ctx := context.Background()

	// owner (not the item owner) cannot report friend's item
	if _, err := ps.Report(ctx, owner.ID, c.ID, item.ID); err == nil {
		t.Fatal("non-owner must not report")
	}
	// friend reports their own item
	r, err := ps.Report(ctx, friend.ID, c.ID, item.ID)
	if err != nil || r.Status != "reported" || r.ReportedAt == nil {
		t.Fatalf("report: %+v err=%v", r, err)
	}
	// friend cannot confirm (only responsible)
	if _, err := ps.Confirm(ctx, friend.ID, c.ID, item.ID); err == nil {
		t.Fatal("non-responsible must not confirm")
	}
	// owner (responsible) confirms
	cf, err := ps.Confirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || cf.Status != "confirmed" || cf.ConfirmedAt == nil {
		t.Fatalf("confirm: %+v err=%v", cf, err)
	}
	// owner un-confirms → back to reported (reported_at was set)
	uc, err := ps.UnConfirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || uc.Status != "reported" || uc.ConfirmedAt != nil {
		t.Fatalf("unconfirm: %+v err=%v", uc, err)
	}
	// friend un-reports → open
	ur, err := ps.UnReport(ctx, friend.ID, c.ID, item.ID)
	if err != nil || ur.Status != "open" || ur.ReportedAt != nil {
		t.Fatalf("unreport: %+v err=%v", ur, err)
	}
}

func TestInvalidTransitionIsBadRequest(t *testing.T) {
	ps, _, c, _, friend, item := paySetupWithItem(t)
	ctx := context.Background()
	// un-report on an open item is invalid
	_, err := ps.UnReport(ctx, friend.ID, c.ID, item.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 invalid transition, got %v", err)
	}
}

func TestDirectConfirmFromOpen(t *testing.T) {
	ps, _, c, owner, _, item := paySetupWithItem(t)
	ctx := context.Background()
	// responsible confirms an open item directly (cash in hand)
	cf, err := ps.Confirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || cf.Status != "confirmed" {
		t.Fatalf("direct confirm: %+v err=%v", cf, err)
	}
	// un-confirm → open (no reported_at)
	uc, err := ps.UnConfirm(ctx, owner.ID, c.ID, item.ID)
	if err != nil || uc.Status != "open" {
		t.Fatalf("unconfirm to open: %+v err=%v", uc, err)
	}
}

func TestAddItemValidatesAmountAndMembership(t *testing.T) {
	ps, gs, q, c, owner := paySetup(t)
	ctx := context.Background()
	member := seedUser(t, q, "member")
	g, _ := q.GetGroup(ctx, c.GroupID)
	_, _ = gs.Join(ctx, member.ID, g.InviteCode)
	stranger := seedUser(t, q, "stranger") // deliberately NOT a group member
	_, _ = ps.Activate(ctx, owner.ID, c.ID, 4500, nil)

	// negative explicit amount is rejected (400)
	neg := int32(-100)
	if _, err := ps.AddItem(ctx, owner.ID, c.ID, member.ID, &neg); err == nil {
		t.Fatal("expected error for negative amount")
	} else if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for negative amount, got %v", err)
	}
	// adding a non-member target is rejected (400 target_not_member)
	if _, err := ps.AddItem(ctx, owner.ID, c.ID, stranger.ID, nil); err == nil {
		t.Fatal("expected error adding non-member")
	} else if e, ok := apperr.As(err); !ok || e.HTTPStatus != 400 {
		t.Fatalf("expected 400 for non-member target, got %v", err)
	}
	// adding a real member at the default amount still works
	if _, err := ps.AddItem(ctx, owner.ID, c.ID, member.ID, nil); err != nil {
		t.Fatalf("valid member add failed: %v", err)
	}
}

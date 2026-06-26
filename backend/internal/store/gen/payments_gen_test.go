package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestPaymentCollectionAndItemLifecycle(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "P1", CreatedBy: owner.ID})
	when := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{GroupID: g.ID, Artist: "A", EventAt: when, RsvpDeadline: when.Add(-time.Hour), CreatedBy: owner.ID})

	col, err := q.CreatePaymentCollection(ctx, gen.CreatePaymentCollectionParams{
		ConcertID: c.ID, ResponsibleUserID: owner.ID, DefaultAmountCents: 4500})
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	item, err := q.CreatePaymentItem(ctx, gen.CreatePaymentItemParams{
		CollectionID: col.ID, UserID: owner.ID, AmountCents: 4500})
	if err != nil || item.Status != "open" {
		t.Fatalf("create item: %v err=%v", item, err)
	}
	now := when
	upd, err := q.UpdatePaymentItemStatus(ctx, gen.UpdatePaymentItemStatusParams{
		ID: item.ID, Status: "reported", ReportedAt: &now, ConfirmedAt: nil})
	if err != nil || upd.Status != "reported" || upd.ReportedAt == nil {
		t.Fatalf("update status: %v err=%v", upd, err)
	}
	rows, err := q.ListPaymentItems(ctx, col.ID)
	if err != nil || len(rows) != 1 || rows[0].DisplayName != "O" {
		t.Fatalf("list items: %v err=%v", rows, err)
	}
}

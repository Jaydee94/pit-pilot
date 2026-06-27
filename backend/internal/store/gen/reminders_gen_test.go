package gen_test

import (
	"context"
	"testing"
	"time"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestReminderScanRecipientsAndIdempotency(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	noResp, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "nr", DisplayName: "NoResp"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G", InviteCode: "RM", CreatedBy: owner.ID})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: owner.ID, Role: "admin"})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: noResp.ID, Role: "member"})

	soon := time.Now().Add(12 * time.Hour) // within the 24h window
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{
		GroupID: g.ID, Artist: "Tool", EventAt: soon.Add(48 * time.Hour),
		RsvpDeadline: soon, CreatedBy: owner.ID})
	// owner says yes; noResp has no rsvp
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: owner.ID, Status: "yes"})

	if err := q.ScanDeadlineReminders(ctx); err != nil {
		t.Fatalf("scan deadline: %v", err)
	}
	rows, _ := q.ClaimPendingNotifications(ctx, 50)
	// exactly one deadline_soon, to the no-RSVP member (not the owner who said yes)
	var deadlineTo []string
	for _, r := range rows {
		if r.Type == "deadline_soon" {
			deadlineTo = append(deadlineTo, r.UserID.String())
		}
	}
	if len(deadlineTo) != 1 || deadlineTo[0] != noResp.ID.String() {
		t.Fatalf("deadline_soon recipients wrong: %v", deadlineTo)
	}
	// idempotency: a second scan inserts nothing new
	before := len(rows)
	if err := q.ScanDeadlineReminders(ctx); err != nil {
		t.Fatalf("scan2: %v", err)
	}
	rows2, _ := q.ClaimPendingNotifications(ctx, 50)
	if len(rows2) != before {
		t.Fatalf("second scan was not idempotent: %d -> %d", before, len(rows2))
	}
}

func TestConcertSoonReminderRecipients(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o2", DisplayName: "O2"})
	yesMember, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "yes", DisplayName: "YesMember"})
	noMember, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "no", DisplayName: "NoMember"})
	g, _ := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "G2", InviteCode: "CS", CreatedBy: owner.ID})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: owner.ID, Role: "admin"})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: yesMember.ID, Role: "member"})
	_ = q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: noMember.ID, Role: "member"})

	// Concert with event_at ~12h from now (within 24h window) and rsvp_deadline in the past
	eventAt := time.Now().Add(12 * time.Hour)
	rsvpDeadline := time.Now().Add(-1 * time.Hour)
	c, _ := q.CreateConcert(ctx, gen.CreateConcertParams{
		GroupID: g.ID, Artist: "Deftones", EventAt: eventAt,
		RsvpDeadline: rsvpDeadline, CreatedBy: owner.ID})
	// yesMember says yes; noMember says no
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: yesMember.ID, Status: "yes"})
	_, _ = q.UpsertRSVP(ctx, gen.UpsertRSVPParams{ConcertID: c.ID, UserID: noMember.ID, Status: "no"})

	if err := q.ScanConcertReminders(ctx); err != nil {
		t.Fatalf("scan concert: %v", err)
	}
	rows, _ := q.ClaimPendingNotifications(ctx, 50)
	// exactly one concert_soon, to the YES member (not the "no" member)
	var concertTo []string
	for _, r := range rows {
		if r.Type == "concert_soon" {
			concertTo = append(concertTo, r.UserID.String())
		}
	}
	if len(concertTo) != 1 || concertTo[0] != yesMember.ID.String() {
		t.Fatalf("concert_soon recipients wrong: got %v, expected [%s]", concertTo, yesMember.ID.String())
	}
	// idempotency: a second scan inserts nothing new
	before := len(rows)
	if err := q.ScanConcertReminders(ctx); err != nil {
		t.Fatalf("scan2: %v", err)
	}
	rows2, _ := q.ClaimPendingNotifications(ctx, 50)
	if len(rows2) != before {
		t.Fatalf("second scan was not idempotent: %d -> %d", before, len(rows2))
	}
}

// backend/internal/service/groups_test.go
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/service"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func seedUser(t *testing.T, q *gen.Queries, sub string) gen.User {
	t.Helper()
	u, err := q.UpsertUser(context.Background(), gen.UpsertUserParams{
		Provider: "google", ProviderSub: sub, DisplayName: sub})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func newGroupSvc(t *testing.T) (*service.GroupService, *gen.Queries) {
	pool := testutil.NewPostgres(t)
	q := gen.New(pool)
	codes := []string{"CODE01", "CODE02", "CODE03"}
	i := 0
	svc := service.NewGroupService(pool, q, func() string { c := codes[i%len(codes)]; i++; return c })
	return svc, q
}

func TestCreateGroupMakesCreatorAdmin(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")

	g, err := svc.Create(ctx, owner.ID, "Festival Squad")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m, err := svc.RequireMembership(ctx, owner.ID, g.ID)
	if err != nil || m.Role != "admin" {
		t.Fatalf("creator should be admin, got %v err=%v", m, err)
	}
}

func TestJoinByCodeAddsMember(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	joiner := seedUser(t, q, "joiner")
	g, _ := svc.Create(ctx, owner.ID, "Crew")

	joined, err := svc.Join(ctx, joiner.ID, g.InviteCode)
	if err != nil || joined.ID != g.ID {
		t.Fatalf("join: %v err=%v", joined, err)
	}
	if _, err := svc.RequireMembership(ctx, joiner.ID, g.ID); err != nil {
		t.Fatalf("joiner should be member: %v", err)
	}
}

func TestJoinUnknownCodeIsNotFound(t *testing.T) {
	svc, q := newGroupSvc(t)
	joiner := seedUser(t, q, "joiner")
	_, err := svc.Join(context.Background(), joiner.ID, "WRONG!")
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

func TestRequireMembershipForbidsNonMember(t *testing.T) {
	svc, q := newGroupSvc(t)
	owner := seedUser(t, q, "owner")
	stranger := seedUser(t, q, "stranger")
	g, _ := svc.Create(context.Background(), owner.ID, "Private")
	_, err := svc.RequireMembership(context.Background(), stranger.ID, g.ID)
	if e, ok := apperr.As(err); !ok || e.HTTPStatus != 403 {
		t.Fatalf("expected 403, got %v", err)
	}
	// non-member cannot access unknown group
	if _, err := svc.RequireMembership(context.Background(), owner.ID, uuid.New()); err != nil {
		if e, ok := apperr.As(err); !ok || e.HTTPStatus != 403 {
			t.Fatalf("expected 403 for unknown group, got %v", err)
		}
	} else {
		t.Fatal("expected error for unknown group membership")
	}
}

func TestRegenerateInviteRequiresAdmin(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	member := seedUser(t, q, "member")
	g, _ := svc.Create(ctx, owner.ID, "Crew")
	_, _ = svc.Join(ctx, member.ID, g.InviteCode)

	if _, err := svc.RegenerateInvite(ctx, member.ID, g.ID); err == nil {
		t.Fatal("member must not regenerate invite")
	}
	g2, err := svc.RegenerateInvite(ctx, owner.ID, g.ID)
	if err != nil || g2.InviteCode == g.InviteCode {
		t.Fatalf("admin regenerate failed: %v err=%v", g2, err)
	}
}

func TestJoinIsIdempotent(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	joiner := seedUser(t, q, "joiner")
	g, _ := svc.Create(ctx, owner.ID, "Crew")
	if _, err := svc.Join(ctx, joiner.ID, g.InviteCode); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := svc.Join(ctx, joiner.ID, g.InviteCode); err != nil {
		t.Fatalf("re-join should be idempotent, got: %v", err)
	}
}

func TestMembersAndListForUser(t *testing.T) {
	svc, q := newGroupSvc(t)
	ctx := context.Background()
	owner := seedUser(t, q, "owner")
	g, _ := svc.Create(ctx, owner.ID, "Crew")

	groups, err := svc.ListForUser(ctx, owner.ID)
	if err != nil || len(groups) != 1 || groups[0].ID != g.ID {
		t.Fatalf("ListForUser: %v err=%v", groups, err)
	}
	members, err := svc.Members(ctx, owner.ID, g.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("Members: %v err=%v", members, err)
	}
	// non-member cannot list members
	stranger := seedUser(t, q, "stranger")
	if _, err := svc.Members(ctx, stranger.ID, g.ID); err == nil {
		t.Fatal("stranger must not list members")
	}
}

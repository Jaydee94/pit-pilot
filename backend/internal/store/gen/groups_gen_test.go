package gen_test

import (
	"context"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
	"github.com/jaydee94/pit-pilot/backend/internal/testutil"
)

func TestListGroupsForUserReturnsJoinedGroups(t *testing.T) {
	q := gen.New(testutil.NewPostgres(t))
	ctx := context.Background()
	owner, _ := q.UpsertUser(ctx, gen.UpsertUserParams{Provider: "google", ProviderSub: "o", DisplayName: "O"})
	g, err := q.CreateGroup(ctx, gen.CreateGroupParams{Name: "Metal", InviteCode: "ABC123", CreatedBy: owner.ID})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: g.ID, UserID: owner.ID, Role: "admin"}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	groups, err := q.ListGroupsForUser(ctx, owner.ID)
	if err != nil || len(groups) != 1 || groups[0].Name != "Metal" {
		t.Fatalf("unexpected groups: %v err=%v", groups, err)
	}
}

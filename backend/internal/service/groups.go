// backend/internal/service/groups.go
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jaydee94/pit-pilot/backend/internal/apperr"
	"github.com/jaydee94/pit-pilot/backend/internal/store/gen"
)

type GroupService struct {
	pool    *pgxpool.Pool
	q       *gen.Queries
	newCode func() string
}

func NewGroupService(pool *pgxpool.Pool, q *gen.Queries, newCode func() string) *GroupService {
	return &GroupService{pool: pool, q: q, newCode: newCode}
}

func (s *GroupService) Create(ctx context.Context, userID uuid.UUID, name string) (gen.Group, error) {
	if name == "" {
		return gen.Group{}, apperr.BadRequest("invalid_name", "group name required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gen.Group{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.q.WithTx(tx)
	g, err := qtx.CreateGroup(ctx, gen.CreateGroupParams{
		Name: name, InviteCode: s.newCode(), CreatedBy: userID})
	if err != nil {
		return gen.Group{}, err
	}
	if err := qtx.AddGroupMember(ctx, gen.AddGroupMemberParams{
		GroupID: g.ID, UserID: userID, Role: "admin"}); err != nil {
		return gen.Group{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return gen.Group{}, err
	}
	return g, nil
}


func (s *GroupService) Get(ctx context.Context, userID, groupID uuid.UUID) (gen.Group, error) {
	if _, err := s.RequireMembership(ctx, userID, groupID); err != nil {
		return gen.Group{}, err
	}
	g, err := s.q.GetGroup(ctx, groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.Group{}, apperr.NotFound("group_not_found", "group not found")
	}
	return g, err
}
func (s *GroupService) ListForUser(ctx context.Context, userID uuid.UUID) ([]gen.Group, error) {
	return s.q.ListGroupsForUser(ctx, userID)
}

func (s *GroupService) Join(ctx context.Context, userID uuid.UUID, code string) (gen.Group, error) {
	g, err := s.q.GetGroupByInviteCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.Group{}, apperr.NotFound("invalid_invite", "invite code not found")
	}
	if err != nil {
		return gen.Group{}, err
	}
	if err := s.q.AddGroupMember(ctx, gen.AddGroupMemberParams{
		GroupID: g.ID, UserID: userID, Role: "member"}); err != nil {
		return gen.Group{}, err
	}
	return g, nil
}

func (s *GroupService) RequireMembership(ctx context.Context, userID, groupID uuid.UUID) (gen.GroupMember, error) {
	m, err := s.q.GetGroupMember(ctx, gen.GetGroupMemberParams{GroupID: groupID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return gen.GroupMember{}, apperr.Forbidden("not_member", "not a member of this group")
	}
	if err != nil {
		return gen.GroupMember{}, err
	}
	return m, nil
}

func (s *GroupService) Members(ctx context.Context, userID, groupID uuid.UUID) ([]gen.ListGroupMembersRow, error) {
	if _, err := s.RequireMembership(ctx, userID, groupID); err != nil {
		return nil, err
	}
	return s.q.ListGroupMembers(ctx, groupID)
}

func (s *GroupService) RegenerateInvite(ctx context.Context, userID, groupID uuid.UUID) (gen.Group, error) {
	m, err := s.RequireMembership(ctx, userID, groupID)
	if err != nil {
		return gen.Group{}, err
	}
	if m.Role != "admin" {
		return gen.Group{}, apperr.Forbidden("not_admin", "admin role required")
	}
	return s.q.UpdateGroupInviteCode(ctx, gen.UpdateGroupInviteCodeParams{
		ID: groupID, InviteCode: s.newCode()})
}

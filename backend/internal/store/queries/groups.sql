-- name: CreateGroup :one
INSERT INTO groups (name, invite_code, created_by)
VALUES ($1, $2, $3) RETURNING *;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: ListGroupsForUser :many
SELECT g.* FROM groups g
JOIN group_members m ON m.group_id = g.id
WHERE m.user_id = $1
ORDER BY g.created_at DESC;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1;

-- name: GetGroupByInviteCode :one
SELECT * FROM groups WHERE invite_code = $1;

-- name: GetGroupMember :one
SELECT * FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.id, u.display_name, u.avatar_url, m.role
FROM group_members m
JOIN users u ON u.id = m.user_id
WHERE m.group_id = $1
ORDER BY m.joined_at;

-- name: UpdateGroupInviteCode :one
UPDATE groups SET invite_code = $2 WHERE id = $1 RETURNING *;

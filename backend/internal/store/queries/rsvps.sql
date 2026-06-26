
-- name: UpsertRSVP :one
INSERT INTO rsvps (concert_id, user_id, status)
VALUES ($1, $2, $3)
ON CONFLICT (concert_id, user_id) DO UPDATE
    SET status = EXCLUDED.status, responded_at = now()
RETURNING *;

-- name: ListRSVPsForConcert :many
SELECT u.id, u.display_name, u.avatar_url, r.status
FROM rsvps r
JOIN users u ON u.id = r.user_id
WHERE r.concert_id = $1
ORDER BY u.display_name;

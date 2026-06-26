
-- name: CreateConcert :one
INSERT INTO concerts
    (group_id, artist, event_at, venue, city, ticket_url, price_cents, notes, rsvp_deadline, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: ListConcertsForGroup :many
SELECT * FROM concerts WHERE group_id = $1 ORDER BY event_at;

-- name: GetConcert :one
SELECT * FROM concerts WHERE id = $1;

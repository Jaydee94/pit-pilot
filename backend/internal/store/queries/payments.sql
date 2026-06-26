-- name: CreatePaymentCollection :one
INSERT INTO payment_collections (concert_id, responsible_user_id, default_amount_cents, payment_link)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetPaymentCollectionByConcert :one
SELECT * FROM payment_collections WHERE concert_id = $1;

-- name: UpdatePaymentCollectionLink :one
UPDATE payment_collections SET payment_link = $2 WHERE id = $1 RETURNING *;

-- name: DeletePaymentCollection :exec
DELETE FROM payment_collections WHERE id = $1;

-- name: CreatePaymentItem :one
INSERT INTO payment_items (collection_id, user_id, amount_cents)
VALUES ($1, $2, $3) RETURNING *;

-- name: GetPaymentItem :one
SELECT * FROM payment_items WHERE id = $1;

-- name: ListPaymentItems :many
SELECT i.id, i.user_id, u.display_name, u.avatar_url,
       i.amount_cents, i.status, i.reported_at, i.confirmed_at
FROM payment_items i
JOIN users u ON u.id = i.user_id
WHERE i.collection_id = $1
ORDER BY u.display_name;

-- name: GetPaymentItemForUser :one
SELECT i.id, i.user_id, u.display_name, u.avatar_url,
       i.amount_cents, i.status, i.reported_at, i.confirmed_at
FROM payment_items i
JOIN users u ON u.id = i.user_id
WHERE i.collection_id = $1 AND i.user_id = $2;

-- name: UpdatePaymentItemAmount :one
UPDATE payment_items SET amount_cents = $2 WHERE id = $1 RETURNING *;

-- name: UpdatePaymentItemStatus :one
UPDATE payment_items
SET status = $2, reported_at = $3, confirmed_at = $4
WHERE id = $1 RETURNING *;

-- name: DeletePaymentItem :exec
DELETE FROM payment_items WHERE id = $1;

-- name: ListYesRsvpUserIDs :many
SELECT user_id FROM rsvps WHERE concert_id = $1 AND status = 'yes';

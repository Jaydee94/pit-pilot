-- name: UpsertPushSubscription :one
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
VALUES ($1, $2, $3, $4)
ON CONFLICT (endpoint) DO UPDATE
    SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth
RETURNING *;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2;

-- name: DeletePushSubscriptionByID :exec
DELETE FROM push_subscriptions WHERE id = $1;

-- name: ListPushSubscriptionsForUser :many
SELECT * FROM push_subscriptions WHERE user_id = $1;

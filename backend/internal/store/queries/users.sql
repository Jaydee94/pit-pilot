-- backend/internal/store/queries/users.sql

-- name: UpsertUser :one
INSERT INTO users (provider, provider_sub, email, display_name, avatar_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider, provider_sub) DO UPDATE
    SET email = EXCLUDED.email,
        display_name = EXCLUDED.display_name,
        avatar_url = EXCLUDED.avatar_url
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

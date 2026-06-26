-- backend/internal/store/migrations/0001_init.up.sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider     text NOT NULL,
    provider_sub text NOT NULL,
    email        text NOT NULL DEFAULT '',
    display_name text NOT NULL DEFAULT '',
    avatar_url   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_sub),
    CHECK (provider IN ('google','apple'))
);

CREATE TABLE groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    invite_code text NOT NULL UNIQUE,
    created_by  uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id  uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role      text NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id),
    CHECK (role IN ('admin','member'))
);

CREATE TABLE concerts (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id      uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    artist        text NOT NULL,
    event_at      timestamptz NOT NULL,
    venue         text,
    city          text,
    ticket_url    text,
    price_cents   integer,
    notes         text,
    rsvp_deadline timestamptz NOT NULL,
    created_by    uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE rsvps (
    concert_id   uuid NOT NULL REFERENCES concerts(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       text NOT NULL,
    responded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (concert_id, user_id),
    CHECK (status IN ('yes','no'))
);

CREATE INDEX idx_concerts_group_event ON concerts (group_id, event_at);
CREATE INDEX idx_group_members_user ON group_members (user_id);

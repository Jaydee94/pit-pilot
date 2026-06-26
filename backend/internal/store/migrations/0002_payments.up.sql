CREATE TABLE payment_collections (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    concert_id           uuid NOT NULL UNIQUE REFERENCES concerts(id) ON DELETE CASCADE,
    responsible_user_id  uuid NOT NULL REFERENCES users(id),
    default_amount_cents integer NOT NULL,
    payment_link         text,
    created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payment_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    collection_id uuid NOT NULL REFERENCES payment_collections(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES users(id),
    amount_cents  integer NOT NULL,
    status        text NOT NULL DEFAULT 'open',
    reported_at   timestamptz,
    confirmed_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (collection_id, user_id),
    CHECK (status IN ('open','reported','confirmed'))
);

CREATE INDEX idx_payment_items_collection ON payment_items (collection_id);

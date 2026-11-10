-- Idempotency keys for critical financial mutations (ADR 0007, README §3 and §6.4).
-- A row is inserted in the same transaction as the operation and only survives if the
-- operation succeeds (2xx); response_status/response_body are filled before commit.
CREATE TABLE idempotency_keys (
    id              uuid PRIMARY KEY,
    household_id    uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    endpoint        text        NOT NULL CHECK (char_length(endpoint) BETWEEN 1 AND 200),
    key             text        NOT NULL CHECK (char_length(key) BETWEEN 1 AND 128),
    request_hash    bytea       NOT NULL CHECK (octet_length(request_hash) = 32),
    response_status smallint CHECK (response_status BETWEEN 200 AND 299),
    -- bytea (not jsonb): replays must return the exact bytes, and jsonb reorders keys (I-41).
    response_body   bytea,
    expires_at      timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, user_id, endpoint, key)
);

-- Supports the cleanup of expired keys (worker, F5.2).
CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);

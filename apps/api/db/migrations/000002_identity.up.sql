-- Identity: users and opaque sessions (ADR 0005). Global tables, no household_id.
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    name          text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    email         citext      NOT NULL UNIQUE CHECK (char_length(email) BETWEEN 3 AND 254),
    password_hash text        NOT NULL,
    is_active     boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- The cookie carries a random token; only its SHA-256 is stored.
-- expires_at slides on use (idle timeout); absolute_expires_at caps the session lifetime.
CREATE TABLE sessions (
    id                  uuid PRIMARY KEY,
    user_id             uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    token_hash          bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at          timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    last_seen_at        timestamptz NOT NULL,
    revoked_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at <= absolute_expires_at)
);

CREATE INDEX sessions_user_id_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;

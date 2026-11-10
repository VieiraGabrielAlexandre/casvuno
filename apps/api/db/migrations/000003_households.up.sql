-- Households, memberships and invites (ADR 0006).
CREATE TABLE households (
    id         uuid PRIMARY KEY,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    timezone   text        NOT NULL DEFAULT 'America/Sao_Paulo' CHECK (char_length(timezone) BETWEEN 1 AND 64),
    currency   text        NOT NULL DEFAULT 'BRL' CHECK (currency = 'BRL'),
    created_by uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- status ACTIVE | INACTIVE only: pending invitations live in household_invites (I-10).
CREATE TABLE household_members (
    id             uuid PRIMARY KEY,
    household_id   uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    role           text        NOT NULL CHECK (role IN ('OWNER', 'MEMBER')),
    status         text        NOT NULL CHECK (status IN ('ACTIVE', 'INACTIVE')),
    joined_at      timestamptz NOT NULL,
    deactivated_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, user_id),
    -- target of composite foreign keys from household-scoped tables
    UNIQUE (household_id, id),
    CHECK ((status = 'INACTIVE') = (deactivated_at IS NOT NULL))
);

CREATE INDEX household_members_user_active_idx ON household_members (user_id) WHERE status = 'ACTIVE';

-- The invite token is a bearer credential: only its SHA-256 is stored. Single use.
CREATE TABLE household_invites (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    email                citext CHECK (email IS NULL OR char_length(email) BETWEEN 3 AND 254),
    token_hash           bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    invited_by_member_id uuid        NOT NULL,
    expires_at           timestamptz NOT NULL,
    accepted_at          timestamptz,
    accepted_by_user_id  uuid REFERENCES users (id) ON DELETE RESTRICT,
    revoked_at           timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (household_id, invited_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    CHECK ((accepted_at IS NULL) = (accepted_by_user_id IS NULL)),
    CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

CREATE INDEX household_invites_pending_idx ON household_invites (household_id, created_at DESC)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

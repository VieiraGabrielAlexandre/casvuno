-- Append-only audit trail (ADR 0012). Written in the same transaction as the audited change.
CREATE TABLE audit_events (
    id            uuid PRIMARY KEY,
    household_id  uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    actor_user_id uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    entity_type   text        NOT NULL CHECK (char_length(entity_type) BETWEEN 1 AND 64),
    entity_id     uuid        NOT NULL,
    action        text        NOT NULL CHECK (char_length(action) BETWEEN 1 AND 64),
    metadata      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_household_created_idx ON audit_events (household_id, created_at DESC);

-- Enforce append-only at the database level.
CREATE FUNCTION audit_events_append_only() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$;

CREATE TRIGGER audit_events_no_update_delete
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

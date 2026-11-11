-- Internal transfers (README §6.3, RN-007, INV-08): one transfer row + exactly two TRANSFER
-- movements (OUT on the origin, IN on the destination), written atomically. Immutable in V1
-- (I-37): a mistake is fixed with an opposite transfer.
CREATE TABLE transfers (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    from_account_id      uuid        NOT NULL,
    to_account_id        uuid        NOT NULL,
    amount_cents         bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    posted_on            date        NOT NULL,
    description          text        NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    created_by_member_id uuid        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, from_account_id) REFERENCES accounts (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, to_account_id) REFERENCES accounts (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, created_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    CHECK (from_account_id <> to_account_id)
);

CREATE INDEX transfers_household_list_idx ON transfers (household_id, posted_on DESC, id DESC);

CREATE TRIGGER transfers_append_only
    BEFORE UPDATE OR DELETE ON transfers
    FOR EACH ROW EXECUTE FUNCTION forbid_update_delete();

ALTER TABLE account_movements
    ADD COLUMN transfer_id uuid,
    ADD CONSTRAINT account_movements_transfer_fk
        FOREIGN KEY (household_id, transfer_id) REFERENCES transfers (household_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT account_movements_transfer_kind_chk CHECK ((kind = 'TRANSFER') = (transfer_id IS NOT NULL));

CREATE INDEX account_movements_transfer_idx ON account_movements (household_id, transfer_id) WHERE transfer_id IS NOT NULL;

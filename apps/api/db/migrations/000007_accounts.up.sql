-- Accounts and cash movements (README §6.2/§6.3, ADR 0002, INV-09, INV-18).
-- Balance = opening_balance_cents + Σ IN − Σ OUT; it is never stored (RN-017).
-- `transfers` and account_movements.transfer_id arrive with the transfer feature (F2.3).

-- Generic guard for append-only financial history (corrections are compensating rows).
CREATE FUNCTION forbid_update_delete() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$;

CREATE TABLE accounts (
    id                    uuid PRIMARY KEY,
    household_id          uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    name                  text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    type                  text        NOT NULL CHECK (type IN ('CHECKING', 'SAVINGS', 'CASH', 'OTHER')),
    ownership             text        NOT NULL CHECK (ownership IN ('INDIVIDUAL', 'JOINT')),
    owner_member_id       uuid,
    -- signed: an account may open overdrawn (I-06); bounded below 2^53 so JSON clients stay exact
    opening_balance_cents bigint      NOT NULL CHECK (opening_balance_cents BETWEEN -1000000000000000 AND 1000000000000000),
    opened_on             date        NOT NULL,
    is_active             boolean     NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, owner_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    CHECK ((ownership = 'INDIVIDUAL') = (owner_member_id IS NOT NULL))
);

CREATE UNIQUE INDEX accounts_active_name_idx ON accounts (household_id, lower(name)) WHERE is_active;

-- No OPENING_ADJUSTMENT kind: the opening balance lives only in accounts (I-06).
CREATE TABLE account_movements (
    id             uuid PRIMARY KEY,
    household_id   uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    account_id     uuid        NOT NULL,
    direction      text        NOT NULL CHECK (direction IN ('IN', 'OUT')),
    amount_cents   bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    posted_on      date        NOT NULL,
    kind           text        NOT NULL CHECK (kind IN ('DIRECT_PAYMENT', 'DIRECT_RECEIPT', 'INVOICE_PAYMENT', 'TRANSFER', 'REVERSAL')),
    reversal_of_id uuid UNIQUE,
    description    text        NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, account_id) REFERENCES accounts (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, reversal_of_id) REFERENCES account_movements (household_id, id) ON DELETE RESTRICT,
    CHECK ((kind = 'REVERSAL') = (reversal_of_id IS NOT NULL)),
    CHECK (reversal_of_id IS NULL OR reversal_of_id <> id)
);

-- Statement (keyset pagination) and balance aggregation.
CREATE INDEX account_movements_statement_idx ON account_movements (household_id, account_id, posted_on DESC, id DESC);

CREATE TRIGGER account_movements_append_only
    BEFORE UPDATE OR DELETE ON account_movements
    FOR EACH ROW EXECUTE FUNCTION forbid_update_delete();

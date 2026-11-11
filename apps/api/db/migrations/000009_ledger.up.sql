-- Ledger (README §6.3, ADR 0002, I-04, I-07, I-08): the economic fact (`transactions`, the
-- only source of consumption), what is owed or receivable directly through an account
-- (`obligations`, commitments outside the card) and how it was settled
-- (`obligation_payments`, each pointing to exactly one cash movement).
-- An immediate entry writes all four rows (transaction, CONFIRMED obligation, payment,
-- movement) in one SQL transaction; a planned one (F3.2) writes only a PLANNED obligation.
-- Card purchases (F4.3) add credit_card_id and CREDIT_CARD and make account_id nullable;
-- recurrences (F5.3) add recurring_occurrence_id (I-09).

-- Target of composite FKs that also pin the kind, so an EXPENSE can never point to an
-- INCOME category (and vice versa) even if a bug skips the service check.
ALTER TABLE categories ADD CONSTRAINT categories_household_id_id_kind_key UNIQUE (household_id, id, kind);

CREATE TABLE transactions (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    kind                 text        NOT NULL CHECK (kind IN ('EXPENSE', 'INCOME')),
    -- classification filters only, never a split between members (RN-003, INV-20)
    scope                text        NOT NULL CHECK (scope IN ('PERSONAL', 'FAMILY')),
    person_member_id     uuid,
    created_by_member_id uuid        NOT NULL,
    description          text        NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    merchant             text CHECK (merchant IS NULL OR char_length(merchant) BETWEEN 1 AND 120),
    notes                text CHECK (notes IS NULL OR char_length(notes) BETWEEN 1 AND 1000),
    amount_cents         bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    currency             text        NOT NULL DEFAULT 'BRL' CHECK (currency = 'BRL'),
    category_id          uuid        NOT NULL,
    occurred_on          date        NOT NULL,
    payment_method       text        NOT NULL CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'OTHER')),
    account_id           uuid        NOT NULL,
    status               text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'VOIDED')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, category_id, kind) REFERENCES categories (household_id, id, kind) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, person_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, created_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    -- a PERSONAL entry names its person (I-43)
    CHECK (scope = 'FAMILY' OR person_member_id IS NOT NULL)
);

-- README §6.3 minimum indexes: period listing and consumption by category.
CREATE INDEX transactions_household_list_idx ON transactions (household_id, occurred_on DESC, id DESC);
CREATE INDEX transactions_household_category_idx ON transactions (household_id, category_id, occurred_on);

CREATE TABLE obligations (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    -- set when confirmed: an immediate entry, or the first payment of a forecast (I-08)
    transaction_id       uuid UNIQUE,
    kind                 text        NOT NULL CHECK (kind IN ('PAYABLE', 'RECEIVABLE')),
    source               text        NOT NULL CHECK (source IN ('DIRECT', 'RECURRING', 'MANUAL_FORECAST')),
    -- lifecycle only; the payment situation is derived from SUM(obligation_payments) (I-02)
    status               text        NOT NULL CHECK (status IN ('PLANNED', 'CONFIRMED', 'CANCELLED')),
    amount_cents         bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    due_on               date        NOT NULL,
    -- draft of the transaction a forecast becomes on its first payment (I-08)
    description          text        NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    category_id          uuid        NOT NULL,
    scope                text        NOT NULL CHECK (scope IN ('PERSONAL', 'FAMILY')),
    person_member_id     uuid,
    payment_method       text        NOT NULL CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'OTHER')),
    account_id           uuid,
    competence_on        date        NOT NULL,
    created_by_member_id uuid        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, transaction_id) REFERENCES transactions (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, category_id) REFERENCES categories (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, person_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, created_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    CHECK (scope = 'FAMILY' OR person_member_id IS NOT NULL),
    -- INV-13 (first half): a forecast has no transaction; a confirmed obligation has one
    CHECK (status = 'CANCELLED' OR (status = 'PLANNED') = (transaction_id IS NULL))
);

CREATE INDEX obligations_household_due_idx ON obligations (household_id, due_on, status);

-- Payments are history: a mistake is undone by reversing the movement (F3.3), never by
-- editing or deleting the payment.
CREATE TABLE obligation_payments (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    obligation_id        uuid        NOT NULL,
    account_movement_id  uuid        NOT NULL UNIQUE,
    amount_cents         bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    paid_on              date        NOT NULL,
    created_by_member_id uuid        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, obligation_id) REFERENCES obligations (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, account_movement_id) REFERENCES account_movements (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, created_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT
);

CREATE INDEX obligation_payments_obligation_idx ON obligation_payments (household_id, obligation_id);

CREATE TRIGGER obligation_payments_append_only
    BEFORE UPDATE OR DELETE ON obligation_payments
    FOR EACH ROW EXECUTE FUNCTION forbid_update_delete();

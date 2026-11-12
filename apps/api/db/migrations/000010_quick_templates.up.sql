-- Quick templates ("favoritos", README §6.3, §9): a member's own shortcuts for frequent
-- entries. Private to the member who created them (I-46), so they belong to the membership,
-- not to the user. Not financial history: deleting one is a real DELETE.
-- Card templates (credit_card_id) arrive with cards (F4.3).
CREATE TABLE quick_templates (
    id                   uuid PRIMARY KEY,
    household_id         uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    member_id            uuid        NOT NULL,
    label                text        NOT NULL CHECK (char_length(label) BETWEEN 1 AND 60),
    kind                 text        NOT NULL CHECK (kind IN ('EXPENSE', 'INCOME')),
    description          text        NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    default_amount_cents bigint CHECK (default_amount_cents IS NULL
                                       OR (default_amount_cents > 0 AND default_amount_cents <= 1000000000000000)),
    category_id          uuid        NOT NULL,
    scope                text        NOT NULL CHECK (scope IN ('PERSONAL', 'FAMILY')),
    person_member_id     uuid,
    payment_method       text        NOT NULL CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'OTHER')),
    account_id           uuid,
    sort_order           integer     NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    -- the category kind is pinned, as in transactions
    FOREIGN KEY (household_id, category_id, kind) REFERENCES categories (household_id, id, kind) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, account_id) REFERENCES accounts (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, person_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    CHECK (scope = 'FAMILY' OR person_member_id IS NOT NULL)
);

CREATE INDEX quick_templates_member_idx ON quick_templates (household_id, member_id, sort_order);

-- "Repeat last" (GET /transactions?createdBy=me&limit=1).
CREATE INDEX transactions_household_author_idx ON transactions (household_id, created_by_member_id, occurred_on DESC, id DESC);

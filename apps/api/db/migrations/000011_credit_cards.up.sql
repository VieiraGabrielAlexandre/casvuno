-- Credit cards (README §6.2, ADR 0003). Never stores the full card number (PAN), the CVV or
-- any bank credential: only an optional last four digits for recognition. Cards are never
-- deleted (archived with is_active = false) so invoices and purchases keep their card.
CREATE TABLE credit_cards (
    id               uuid PRIMARY KEY,
    household_id     uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    name             text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    holder_member_id uuid        NOT NULL,
    last_four        text CHECK (last_four IS NULL OR last_four ~ '^[0-9]{4}$'),
    -- estimated limit (RN-018, I-17); bounded below 2^53 so JSON clients stay exact
    limit_cents      bigint      NOT NULL CHECK (limit_cents BETWEEN 1 AND 1000000000000000),
    closing_day      smallint    NOT NULL CHECK (closing_day BETWEEN 1 AND 31),
    due_day          smallint    NOT NULL CHECK (due_day BETWEEN 1 AND 31),
    is_active        boolean     NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    -- target of composite foreign keys (invoices, purchases, recurring rules)
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, holder_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT
);

-- Names are unique among the household's active cards (case-insensitive).
CREATE UNIQUE INDEX credit_cards_active_name_idx ON credit_cards (household_id, lower(name)) WHERE is_active;

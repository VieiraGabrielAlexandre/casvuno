-- Card invoices and installments (ADR 0002 I-04, ADR 0003) and card purchases in the ledger.
-- A purchase on the card is ONE transaction (consumption, counted once) and N installments,
-- each one a line of an invoice. Installments are not obligations: an invoice is paid as a
-- whole (F4.5), so commitments use the invoice balance and never sum installments again.

-- Invoices are created on demand when a purchase needs them (find-or-create under the card
-- lock), with concrete dates: changing the card's days only affects invoices not created yet.
CREATE TABLE card_invoices (
    id                       uuid PRIMARY KEY,
    household_id             uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    credit_card_id           uuid        NOT NULL,
    -- purchases on [cycle_start_on, closing_on) belong to this invoice (RN-011)
    cycle_start_on           date        NOT NULL,
    closing_on               date        NOT NULL,
    due_on                   date        NOT NULL,
    -- lifecycle only; the payment situation is derived from the balance (I-03)
    status                   text        NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED')),
    -- frozen when the invoice is closed (F4.4, INV-15)
    closed_base_amount_cents bigint CHECK (closed_base_amount_cents BETWEEN 0 AND 1000000000000000),
    closed_at                timestamptz,
    closed_by_member_id      uuid,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    -- target of the installments' FK, which pins the invoice to the purchase's card
    UNIQUE (household_id, credit_card_id, id),
    UNIQUE (credit_card_id, cycle_start_on),
    UNIQUE (credit_card_id, closing_on),
    FOREIGN KEY (household_id, credit_card_id) REFERENCES credit_cards (household_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, closed_by_member_id) REFERENCES household_members (household_id, id) ON DELETE RESTRICT,
    CHECK (cycle_start_on < closing_on AND closing_on < due_on),
    CHECK ((status = 'CLOSED') = (closed_base_amount_cents IS NOT NULL)),
    CHECK ((status = 'CLOSED') = (closed_at IS NOT NULL)),
    CHECK ((status = 'CLOSED') = (closed_by_member_id IS NOT NULL))
);

-- README §6.3 minimum index: invoices of a card by due date.
CREATE INDEX card_invoices_card_due_idx ON card_invoices (household_id, credit_card_id, due_on);

-- Card purchases: the card replaces the account as the source (I-07). Income never goes to a card.
ALTER TABLE transactions
    ADD COLUMN credit_card_id uuid,
    ALTER COLUMN account_id DROP NOT NULL,
    DROP CONSTRAINT transactions_payment_method_check,
    ADD CONSTRAINT transactions_payment_method_check
        CHECK (payment_method IN ('PIX', 'DEBIT', 'CASH', 'BANK_TRANSFER', 'BOLETO', 'CREDIT_CARD', 'OTHER')),
    ADD CONSTRAINT transactions_credit_card_fk
        FOREIGN KEY (household_id, credit_card_id) REFERENCES credit_cards (household_id, id) ON DELETE RESTRICT,
    -- exactly one source: the card (CREDIT_CARD) or an account (every other method)
    ADD CONSTRAINT transactions_card_method_check CHECK ((credit_card_id IS NOT NULL) = (payment_method = 'CREDIT_CARD')),
    ADD CONSTRAINT transactions_single_source_check CHECK ((credit_card_id IS NULL) = (account_id IS NOT NULL)),
    ADD CONSTRAINT transactions_income_card_check CHECK (kind = 'EXPENSE' OR credit_card_id IS NULL),
    -- target of the installments' FK, which pins the installment to the purchase's card
    ADD CONSTRAINT transactions_household_id_id_card_key UNIQUE (household_id, id, credit_card_id);

CREATE INDEX transactions_household_card_idx ON transactions (household_id, credit_card_id, occurred_on)
    WHERE credit_card_id IS NOT NULL;

-- One row per installment: a line of one invoice (I-04). The composite FKs make the database
-- refuse an installment whose invoice or transaction belongs to another card or household.
CREATE TABLE card_installments (
    id                 uuid PRIMARY KEY,
    household_id       uuid        NOT NULL REFERENCES households (id) ON DELETE RESTRICT,
    transaction_id     uuid        NOT NULL,
    credit_card_id     uuid        NOT NULL,
    card_invoice_id    uuid        NOT NULL,
    installment_number smallint    NOT NULL,
    installment_total  smallint    NOT NULL CHECK (installment_total BETWEEN 1 AND 60),
    amount_cents       bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000000),
    -- CANCELLED only by voiding the purchase while the invoice is OPEN (F4.6, RN-019)
    status             text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'CANCELLED')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    UNIQUE (transaction_id, installment_number),
    CHECK (installment_number BETWEEN 1 AND installment_total),
    FOREIGN KEY (household_id, transaction_id, credit_card_id)
        REFERENCES transactions (household_id, id, credit_card_id) ON DELETE RESTRICT,
    FOREIGN KEY (household_id, credit_card_id, card_invoice_id)
        REFERENCES card_invoices (household_id, credit_card_id, id) ON DELETE RESTRICT
);

-- Invoice detail and totals (F4.4) read the installments of one invoice.
CREATE INDEX card_installments_invoice_idx ON card_installments (household_id, card_invoice_id);
